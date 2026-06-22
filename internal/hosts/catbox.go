package hosts

import (
	"bytes"
	"context"
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

type CatboxHost struct {
	config config.Config
	client *http.Client
}

// NewCatboxHost inicializa um host com as configurações atuais do perfil
func NewCatboxHost(cfg config.Config) *CatboxHost {
	return &CatboxHost{
		config: cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (h *CatboxHost) Name() string {
	return "Catbox"
}

// UploadImage faz o upload para o Catbox.moe com até 3 retentativas e Backoff Exponencial
func (h *CatboxHost) UploadImage(ctx context.Context, fpath string) (models.UploadResult, error) {
	maxRetries := 3
	var lastErr error

	for i := 0; i < maxRetries; i++ {
		res, err := h.doUpload(ctx, fpath)
		if err == nil {
			return res, nil
		}
		lastErr = err

		// Espera exponencial: 2s, 4s, 8s...
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

// doUpload encapsula a chamada HTTP pura
func (h *CatboxHost) doUpload(ctx context.Context, fpath string) (models.UploadResult, error) {
	file, err := os.Open(fpath)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer file.Close()

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	w.WriteField("reqtype", "fileupload")
	if h.config.HostToken != "" {
		w.WriteField("userhash", h.config.HostToken)
	}

	fw, err := w.CreateFormFile("fileToUpload", filepath.Base(fpath))
	if err != nil {
		return models.UploadResult{}, err
	}

	if _, err = io.Copy(fw, file); err != nil {
		return models.UploadResult{}, err
	}

	if err = w.Close(); err != nil {
		return models.UploadResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://catbox.moe/user/api.php", &b)
	if err != nil {
		return models.UploadResult{}, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())

	resp, err := h.client.Do(req)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return models.UploadResult{}, err
	}

	if resp.StatusCode != http.StatusOK {
		return models.UploadResult{}, fmt.Errorf("erro do servidor %d: %s", resp.StatusCode, string(body))
	}

	return models.UploadResult{
		URL:      string(body),
		Filename: filepath.Base(fpath),
		Success:  true,
	}, nil
}

// CreateAlbum cria um álbum no catbox (não totalmente implementado até precisarmos)
func (h *CatboxHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}
