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
	"strings"
	"time"

	"barfimanga/internal/config"
	"barfimanga/internal/models"
)

type ImgHippoHost struct {
	config config.Config
	client *http.Client
}

func NewImgHippoHost(cfg config.Config) *ImgHippoHost {
	return &ImgHippoHost{
		config: cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (h *ImgHippoHost) Name() string {
	return "ImgHippo"
}

func (h *ImgHippoHost) UploadImage(ctx context.Context, fpath string) (models.UploadResult, error) {
	maxRetries := 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		res, err := h.doUpload(ctx, fpath)
		if err == nil {
			return res, nil
		}
		lastErr = err

		select {
		case <-ctx.Done():
			return models.UploadResult{}, ctx.Err()
		case <-time.After(time.Duration(1<<i) * 2 * time.Second):
		}
	}

	return models.UploadResult{
		Filename: filepath.Base(fpath),
		Success:  false,
		Error:    lastErr.Error(),
	}, lastErr
}

type imgHippoResponse struct {
	Success bool `json:"success"`
	Data    struct {
		ViewURL string `json:"view_url"`
	} `json:"data"`
	Message string `json:"message"`
}

func (h *ImgHippoHost) doUpload(ctx context.Context, fpath string) (models.UploadResult, error) {
	if h.config.HostToken == "" {
		return models.UploadResult{}, fmt.Errorf("host_token (API Key) não configurado para ImgHippo")
	}

	file, err := os.Open(fpath)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer file.Close()

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	w.WriteField("api_key", h.config.HostToken)
	w.WriteField("title", strings.TrimSuffix(filepath.Base(fpath), filepath.Ext(fpath)))

	fw, err := w.CreateFormFile("file", filepath.Base(fpath))
	if err != nil {
		return models.UploadResult{}, err
	}
	if _, err = io.Copy(fw, file); err != nil {
		return models.UploadResult{}, err
	}
	if err = w.Close(); err != nil {
		return models.UploadResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.imghippo.com/v1/upload", &b)
	if err != nil {
		return models.UploadResult{}, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := h.client.Do(req)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return models.UploadResult{}, fmt.Errorf("erro http %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result imgHippoResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return models.UploadResult{}, err
	}

	if !result.Success {
		return models.UploadResult{}, fmt.Errorf("api error: %s", result.Message)
	}

	return models.UploadResult{
		URL:      result.Data.ViewURL,
		Filename: filepath.Base(fpath),
		Success:  true,
	}, nil
}

func (h *ImgHippoHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}
