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
	"time"

	"barfimanga/internal/config"
	"barfimanga/internal/models"
)

type PixeldrainHost struct {
	config config.Config
	client *http.Client
}

func NewPixeldrainHost(cfg config.Config) *PixeldrainHost {
	return &PixeldrainHost{
		config: cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (h *PixeldrainHost) Name() string {
	return "Pixeldrain"
}

func (h *PixeldrainHost) UploadImage(ctx context.Context, fpath string) (models.UploadResult, error) {
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

type pixeldrainResponse struct {
	Success bool   `json:"success"`
	ID      string `json:"id"`
	Message string `json:"message"`
}

func (h *PixeldrainHost) doUpload(ctx context.Context, fpath string) (models.UploadResult, error) {
	file, err := os.Open(fpath)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer file.Close()

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

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

	req, err := http.NewRequestWithContext(ctx, "POST", "https://pixeldrain.com/api/file", &b)
	if err != nil {
		return models.UploadResult{}, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	if h.config.HostToken != "" {
		req.SetBasicAuth("", h.config.HostToken) // Pixeldrain usa key no username/password (geralmente password)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 201 {
		return models.UploadResult{}, fmt.Errorf("erro http %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result pixeldrainResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return models.UploadResult{}, err
	}

	if result.ID == "" && !result.Success {
		return models.UploadResult{}, fmt.Errorf("api error: %s", result.Message)
	}

	return models.UploadResult{
		URL:      "https://pixeldrain.com/api/file/" + result.ID,
		Filename: filepath.Base(fpath),
		Success:  true,
	}, nil
}

func (h *PixeldrainHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}
