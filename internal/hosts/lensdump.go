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

type LensdumpHost struct {
	config config.Config
	client *http.Client
}

func NewLensdumpHost(cfg config.Config) *LensdumpHost {
	return &LensdumpHost{
		config: cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (h *LensdumpHost) Name() string {
	return "Lensdump"
}

type lensdumpResponse struct {
	StatusCode int `json:"status_code"`
	Image      struct {
		URL string `json:"url"`
	} `json:"image"`
	Error struct {
		Message string `json:"message"`
	} `json:"error"`
}

// UploadImage faz upload pro Lensdump. Uma tentativa só — retry e backoff já
// são responsabilidade do worker.Pool (que envolve todo host igual).
func (h *LensdumpHost) UploadImage(ctx context.Context, fpath string) (models.UploadResult, error) {
	file, err := os.Open(fpath)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer file.Close()

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	fw, err := w.CreateFormFile("source", filepath.Base(fpath))
	if err != nil {
		return models.UploadResult{}, err
	}
	if _, err = io.Copy(fw, file); err != nil {
		return models.UploadResult{}, err
	}
	if err = w.Close(); err != nil {
		return models.UploadResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://lensdump.com/api/1/upload", &b)
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

	var result lensdumpResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return models.UploadResult{}, err
	}

	if result.StatusCode != 200 {
		return models.UploadResult{}, fmt.Errorf("api error: %s", result.Error.Message)
	}

	return models.UploadResult{
		URL:      result.Image.URL,
		Filename: filepath.Base(fpath),
		Success:  true,
	}, nil
}

func (h *LensdumpHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}
