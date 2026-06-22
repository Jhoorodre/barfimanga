package hosts

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"barfimanga/internal/config"
	"barfimanga/internal/models"
)

type ImgBBHost struct {
	config config.Config
	client *http.Client
}

func NewImgBBHost(cfg config.Config) *ImgBBHost {
	return &ImgBBHost{
		config: cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (h *ImgBBHost) Name() string {
	return "ImgBB"
}

func (h *ImgBBHost) UploadImage(ctx context.Context, fpath string) (models.UploadResult, error) {
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

type imgbbResponse struct {
	Success bool `json:"success"`
	Data    struct {
		URL string `json:"url"`
	} `json:"data"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

func (h *ImgBBHost) doUpload(ctx context.Context, fpath string) (models.UploadResult, error) {
	if h.config.HostToken == "" {
		return models.UploadResult{}, fmt.Errorf("host_token (API Key) não configurado para ImgBB")
	}

	imgData, err := os.ReadFile(fpath)
	if err != nil {
		return models.UploadResult{}, err
	}

	encoded := base64.StdEncoding.EncodeToString(imgData)
	fileName := strings.TrimSuffix(filepath.Base(fpath), filepath.Ext(fpath))

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	w.WriteField("key", h.config.HostToken)
	w.WriteField("image", encoded)
	w.WriteField("name", fileName)

	if err = w.Close(); err != nil {
		return models.UploadResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.imgbb.com/1/upload", &b)
	if err != nil {
		return models.UploadResult{}, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := h.client.Do(req)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer resp.Body.Close()

	var result imgbbResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return models.UploadResult{}, err
	}

	if !result.Success {
		return models.UploadResult{}, fmt.Errorf("imgbb api error: %s", result.Error.Message)
	}

	return models.UploadResult{
		URL:      result.Data.URL,
		Filename: filepath.Base(fpath),
		Success:  true,
	}, nil
}

func (h *ImgBBHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	// ImgBB API não suporta álbuns.
	return "", nil
}
