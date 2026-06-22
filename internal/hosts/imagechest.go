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

type ImageChestHost struct {
	config config.Config
	client *http.Client
}

func NewImageChestHost(cfg config.Config) *ImageChestHost {
	return &ImageChestHost{
		config: cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (h *ImageChestHost) Name() string {
	return "ImageChest"
}

func (h *ImageChestHost) UploadImage(ctx context.Context, fpath string) (models.UploadResult, error) {
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

type imageChestResponse struct {
	Success bool `json:"success"`
	Data    struct {
		URL string `json:"url"`
	} `json:"data"`
	Message string `json:"message"`
}

func (h *ImageChestHost) doUpload(ctx context.Context, fpath string) (models.UploadResult, error) {
	if h.config.HostToken == "" {
		return models.UploadResult{}, fmt.Errorf("host_token (API Key) não configurado para ImageChest")
	}

	file, err := os.Open(fpath)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer file.Close()

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	fw, err := w.CreateFormFile("image", filepath.Base(fpath))
	if err != nil {
		return models.UploadResult{}, err
	}
	if _, err = io.Copy(fw, file); err != nil {
		return models.UploadResult{}, err
	}
	if err = w.Close(); err != nil {
		return models.UploadResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.imagechest.com/v1/upload", &b)
	if err != nil {
		return models.UploadResult{}, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+h.config.HostToken)

	resp, err := h.client.Do(req)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return models.UploadResult{}, fmt.Errorf("erro http %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result imageChestResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return models.UploadResult{}, err
	}

	if !result.Success {
		return models.UploadResult{}, fmt.Errorf("api error: %s", result.Message)
	}

	return models.UploadResult{
		URL:      result.Data.URL,
		Filename: filepath.Base(fpath),
		Success:  true,
	}, nil
}

func (h *ImageChestHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}
