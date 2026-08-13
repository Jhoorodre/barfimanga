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

type SxcuHost struct {
	config config.Config
	client *http.Client
}

func NewSxcuHost(cfg config.Config) *SxcuHost {
	return &SxcuHost{
		config: cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (h *SxcuHost) Name() string {
	return "sxcu.net"
}

type sxcuResponse struct {
	URL   string `json:"url"`
	Error string `json:"error"`
}

// UploadImage sobe pro sxcu.net — anônimo, sem token (voltado pra ShareX e
// afins: https://sxcu.net/api/docs). "noembed" é obrigatório pra API devolver
// o link direto do arquivo em vez da página de visualização. Rate limit
// deles é baixo (3 req/min nesse endpoint) — configure rate_limit ~0.05 no
// perfil se for usar esse host pra lote grande.
func (h *SxcuHost) UploadImage(ctx context.Context, fpath string) (models.UploadResult, error) {
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
	if err = w.WriteField("noembed", "1"); err != nil {
		return models.UploadResult{}, err
	}
	if err = w.Close(); err != nil {
		return models.UploadResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://sxcu.net/api/files/create", &b)
	if err != nil {
		return models.UploadResult{}, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	// A API exige User-Agent válido (erro 803 sem isso).
	req.Header.Set("User-Agent", "BarfiManga/1.0 (+https://github.com/Jhoorodre/barfimanga)")

	resp, err := h.client.Do(req)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return models.UploadResult{}, fmt.Errorf("erro http %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result sxcuResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return models.UploadResult{}, err
	}
	if result.URL == "" {
		return models.UploadResult{}, fmt.Errorf("resposta do sxcu.net sem url: %s", string(bodyBytes))
	}

	return models.UploadResult{
		URL:      result.URL,
		Filename: filepath.Base(fpath),
		Success:  true,
	}, nil
}

func (h *SxcuHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}
