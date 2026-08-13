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

// getServer recupera um servidor de upload disponível.
// API atual: GET /servers (o antigo /getServer não existe mais, dá 404).
func (h *GofileHost) getServer(ctx context.Context) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", "https://api.gofile.io/servers", nil)
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
			Servers []struct {
				Name string `json:"name"`
			} `json:"servers"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}

	if result.Status != "ok" || len(result.Data.Servers) == 0 {
		return "", fmt.Errorf("status inesperado da API: %s", result.Status)
	}

	// API atual: POST /contents/uploadfile no servidor escolhido (o antigo
	// /uploadFile não existe mais).
	return fmt.Sprintf("https://%s.gofile.io/contents/uploadfile", result.Data.Servers[0].Name), nil
}

// UploadImage sobe pro GoFile. Funciona anônimo (sem HostToken), mas nesse
// caso a URL devolvida é a PÁGINA de download (gofile.io/d/XXXXX), não a
// imagem direta — confirmado ao vivo: o endpoint que resolve o link direto
// (getDirectLink) devolve 401 "error-notPremium" pra conta gratuita/anônima,
// mesmo com o guestToken que o próprio upload gera. Só funciona com
// HostToken de uma conta GoFile Premium de verdade.
func (h *GofileHost) UploadImage(ctx context.Context, filePath string) (models.UploadResult, error) {
	serverURL, err := h.getServer(ctx)
	if err != nil {
		serverURL = "https://store1.gofile.io/contents/uploadfile" // fallback se /servers falhar
	}

	file, err := os.Open(filePath)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer file.Close()

	var b bytes.Buffer
	writer := multipart.NewWriter(&b)
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

	req, err := http.NewRequestWithContext(ctx, "POST", serverURL, &b)
	if err != nil {
		return models.UploadResult{}, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	if h.config.HostToken != "" {
		req.Header.Set("Authorization", "Bearer "+h.config.HostToken)
	}

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
			ID           string `json:"id"`
			DownloadPage string `json:"downloadPage"`
		} `json:"data"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return models.UploadResult{}, err
	}

	if result.Status != "ok" {
		return models.UploadResult{}, fmt.Errorf("erro do GoFile: %s", result.Status)
	}

	finalURL := result.Data.DownloadPage
	if h.config.HostToken != "" {
		if directURL := h.getDirectLink(ctx, result.Data.ID); directURL != "" {
			finalURL = directURL
		}
	}

	return models.UploadResult{
		URL:      finalURL,
		Filename: filepath.Base(filePath),
		Success:  true,
	}, nil
}

// getDirectLink busca o link direto do arquivo. Exige conta Premium — API
// atual: GET /contents/{id} com Authorization: Bearer <token da conta>.
func (h *GofileHost) getDirectLink(ctx context.Context, contentID string) string {
	contentURL := fmt.Sprintf("https://api.gofile.io/contents/%s", contentID)

	req, err := http.NewRequestWithContext(ctx, "GET", contentURL, nil)
	if err != nil {
		return ""
	}
	req.Header.Set("Authorization", "Bearer "+h.config.HostToken)

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

	if result.Status == "ok" && result.Data.Type == "file" {
		if result.Data.DirectLink != "" {
			return result.Data.DirectLink
		}
		if result.Data.Server != "" && result.Data.Name != "" {
			return fmt.Sprintf("https://%s.gofile.io/download/%s/%s", result.Data.Server, contentID, result.Data.Name)
		}
	}

	return ""
}

func (h *GofileHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	// A API do Gofile exige endpoints diferentes para criar "Folders" autenticados
	return "", nil
}
