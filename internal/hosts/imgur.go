package hosts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"barfimanga/internal/config"
	"barfimanga/internal/models"
)

type ImgurHost struct {
	config config.Config
	client *http.Client
}

func NewImgurHost(cfg config.Config) *ImgurHost {
	return &ImgurHost{
		config: cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (h *ImgurHost) Name() string {
	return "Imgur"
}

func (h *ImgurHost) UploadImage(ctx context.Context, fpath string) (models.UploadResult, error) {
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

type imgurResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Link  string `json:"link"`
		Error string `json:"error"`
	} `json:"data"`
}

func (h *ImgurHost) doUpload(ctx context.Context, fpath string) (models.UploadResult, error) {
	if h.config.HostToken == "" {
		return models.UploadResult{}, fmt.Errorf("host_token (Client-ID ou Bearer) não configurado para Imgur")
	}

	imgData, err := os.ReadFile(fpath)
	if err != nil {
		return models.UploadResult{}, err
	}

	encoded := base64.StdEncoding.EncodeToString(imgData)
	payload := map[string]string{
		"image": encoded,
		"type":  "base64",
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return models.UploadResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.imgur.com/3/image", bytes.NewReader(body))
	if err != nil {
		return models.UploadResult{}, err
	}

	req.Header.Set("Content-Type", "application/json")
	
	// Imgur aceita Client-ID ou Bearer token (para contas)
	if len(h.config.HostToken) > 15 { // Muito provavelmente um Client-ID
		req.Header.Set("Authorization", "Client-ID "+h.config.HostToken)
	} else {
		req.Header.Set("Authorization", "Bearer "+h.config.HostToken)
	}

	resp, err := h.client.Do(req)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode == 429 {
		return models.UploadResult{}, fmt.Errorf("rate limited pelo Imgur (HTTP 429)")
	}

	var result imgurResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return models.UploadResult{}, err
	}

	if !result.Success {
		return models.UploadResult{}, fmt.Errorf("imgur api error: %s", result.Data.Error)
	}

	return models.UploadResult{
		URL:      result.Data.Link,
		Filename: filepath.Base(fpath),
		Success:  true,
	}, nil
}

func (h *ImgurHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil // Pode ser implementado no futuro se houver token OAuth em vez de apenas Client-ID
}
