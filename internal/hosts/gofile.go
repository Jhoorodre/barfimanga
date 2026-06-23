package hosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"

	"barfimanga/internal/config"
	"barfimanga/internal/models"
)

type GofileHost struct {
	config config.Config
	client *http.Client
}

func NewGofileHost(cfg config.Config) *GofileHost {
	return &GofileHost{
		config: cfg,
		client: &http.Client{},
	}
}

func (h *GofileHost) Name() string {
	return "GoFile"
}

// getServer recupera a melhor URL de servidor disponível no GoFile para upload
func (h *GofileHost) getServer(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.gofile.io/getServer", nil)
	if err != nil {
		return "", err
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("falha ao obter servidor GoFile, status: %d", resp.StatusCode)
	}

	var result struct {
		Status string `json:"status"`
		Data   struct {
			Server string `json:"server"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Status != "ok" {
		return "", fmt.Errorf("status inesperado da API: %s", result.Status)
	}

	return fmt.Sprintf("https://%s.gofile.io/uploadFile", result.Data.Server), nil
}

func (h *GofileHost) UploadImage(ctx context.Context, filePath string) (models.UploadResult, error) {
	// 1. Pega a URL do Server
	serverURL, err := h.getServer(ctx)
	if err != nil {
		// Fallback para URL base como no script original
		serverURL = "https://store1.gofile.io/uploadFile"
	}

	// 2. Prepara o arquivo
	file, err := os.Open(filePath)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer file.Close()

	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Se houver um token nas configs, pode-se passar o token para o upload ser atrelado à conta
	if h.config.HostToken != "" {
		writer.WriteField("token", h.config.HostToken)
	}

	part, err := writer.CreateFormFile("file", filepath.Base(filePath))
	if err != nil {
		return models.UploadResult{}, err
	}

	if _, err = io.Copy(part, file); err != nil {
		return models.UploadResult{}, err
	}

	if err = writer.Close(); err != nil {
		return models.UploadResult{}, err
	}

	// 3. Envia o Upload
	req, err := http.NewRequestWithContext(ctx, "POST", serverURL, body)
	if err != nil {
		return models.UploadResult{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := h.client.Do(req)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		b, _ := io.ReadAll(resp.Body)
		return models.UploadResult{}, fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(b))
	}

	var result struct {
		Status string `json:"status"`
		Data   struct {
			Code         string `json:"code"`
			DownloadPage string `json:"downloadPage"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return models.UploadResult{}, err
	}

	if result.Status != "ok" {
		return models.UploadResult{}, fmt.Errorf("erro do GoFile: %s", result.Status)
	}

	// Tentar obter o link direto (Direct Link) assim como no python
	directURL := h.getDirectLink(ctx, result.Data.Code)
	finalURL := result.Data.DownloadPage
	if directURL != "" {
		finalURL = directURL
	}

	return models.UploadResult{
		URL:      finalURL,
		Filename: filepath.Base(filePath),
		Success:  true,
		Error:    "",
	}, nil
}

func (h *GofileHost) getDirectLink(ctx context.Context, fileCode string) string {
	contentURL := fmt.Sprintf("https://api.gofile.io/getContent?contentId=%s", fileCode)

	req, err := http.NewRequestWithContext(ctx, "GET", contentURL, nil)
	if err != nil {
		return ""
	}

	// É necessário o token para `getContent`? Gofile diz que sim para dados completos de conta,
	// mas pastas publicas podem responder.
	if h.config.HostToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.config.HostToken)
	}

	resp, err := h.client.Do(req)
	if err != nil || resp.StatusCode != http.StatusOK {
		return ""
	}
	defer resp.Body.Close()

	var result struct {
		Status string `json:"status"`
		Data   struct {
			Type       string `json:"type"`
			DirectLink string `json:"directLink"`
			Server     string `json:"server"`
			Name       string `json:"name"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return ""
	}

	if result.Status == "ok" {
		if result.Data.Type == "file" {
			if result.Data.DirectLink != "" {
				return result.Data.DirectLink
			}
			if result.Data.Server != "" && result.Data.Name != "" {
				return fmt.Sprintf("https://%s.gofile.io/download/%s/%s", result.Data.Server, fileCode, result.Data.Name)
			}
		}
	}

	return ""
}

func (h *GofileHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	// A API do Gofile exige endpoints diferentes para criar "Folders" autenticados
	return "", nil
}
