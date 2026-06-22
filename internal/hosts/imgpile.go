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

type ImgPileHost struct {
	config config.Config
	client *http.Client
}

func NewImgPileHost(cfg config.Config) *ImgPileHost {
	return &ImgPileHost{
		config: cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (h *ImgPileHost) Name() string {
	return "ImgPile"
}

func (h *ImgPileHost) UploadImage(ctx context.Context, fpath string) (models.UploadResult, error) {
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

func (h *ImgPileHost) doUpload(ctx context.Context, fpath string) (models.UploadResult, error) {
	file, err := os.Open(fpath)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer file.Close()

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	w.WriteField("title", strings.TrimSuffix(filepath.Base(fpath), filepath.Ext(fpath)))
	if h.config.HostToken != "" {
		w.WriteField("api_key", h.config.HostToken)
	}

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

	req, err := http.NewRequestWithContext(ctx, "POST", "https://imgpile.com/api/images", &b)
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

	var result map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return models.UploadResult{}, err
	}

	if id, ok := result["id"].(string); ok {
		ext := "jpg"
		if extVal, ok := result["extension"].(string); ok {
			ext = extVal
		} else {
			ext = strings.TrimPrefix(filepath.Ext(fpath), ".")
		}
		return models.UploadResult{
			URL:      fmt.Sprintf("https://imgpile.com/static/uploads/%s.%s", id, ext),
			Filename: filepath.Base(fpath),
			Success:  true,
		}, nil
	} else if url, ok := result["url"].(string); ok {
		return models.UploadResult{
			URL:      url,
			Filename: filepath.Base(fpath),
			Success:  true,
		}, nil
	}

	return models.UploadResult{}, fmt.Errorf("resposta inesperada: %s", string(bodyBytes))
}

func (h *ImgPileHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}
