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
	"strconv"
	"sync/atomic"
	"time"

	"barfimanga/internal/config"
	"barfimanga/internal/models"
)

type SxcuHost struct {
	config config.Config
	client *http.Client

	// quotaWaits/quotaWaitNanos acumulam ciclos de rate-limit (429) e o tempo
	// gasto neles — mesma ideia do ImgChestHost, ver hosts.QuotaReporter.
	quotaWaits     atomic.Int64
	quotaWaitNanos atomic.Int64
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
	// A API exige User-Agent válido (erro 803 sem isso); formato sugerido
	// pela doc deles: sxcuUploader/$versao (+$url).
	req.Header.Set("User-Agent", "sxcuUploader/1.0 (+https://github.com/Jhoorodre/barfimanga)")

	resp, err := h.client.Do(req)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode == http.StatusTooManyRequests {
			h.waitForRateLimitHeader(ctx, resp.Header)
		}
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

// waitForRateLimitHeader dorme o tempo indicado por X-RateLimit-Reset-After
// (documentado na API deles) quando um 429 acontece — o rate limit desse
// endpoint é agressivo (3 req/min), então sem isso o worker.Pool ficaria
// tentando de novo cegamente com backoff genérico. Ignora silenciosamente
// se o header não vier ou não for um número válido.
func (h *SxcuHost) waitForRateLimitHeader(ctx context.Context, header http.Header) {
	raw := header.Get("X-RateLimit-Reset-After")
	if raw == "" {
		return
	}
	seconds, err := strconv.ParseFloat(raw, 64)
	if err != nil || seconds <= 0 {
		return
	}
	wait := time.Duration(seconds * float64(time.Second))
	if wait > maxRetryAfterWait {
		wait = maxRetryAfterWait
	}

	h.quotaWaits.Add(1)
	h.quotaWaitNanos.Add(int64(wait))
	resumeAt := time.Now().Add(wait)
	fmt.Fprintf(os.Stderr, "\n   [sxcu.net] rate limit atingido — aguardando %s (retoma às %s)\n",
		wait.Round(100*time.Millisecond), resumeAt.Format("15:04:05"))

	select {
	case <-time.After(wait):
		fmt.Fprintln(os.Stderr, "   [sxcu.net] rate limit liberado, retomando uploads.")
	case <-ctx.Done():
	}
}

// QuotaCycles reporta quantos ciclos de rate-limit ocorreram e o tempo total
// gasto neles — ver hosts.QuotaReporter.
func (h *SxcuHost) QuotaCycles() (waits int, totalWait time.Duration) {
	return int(h.quotaWaits.Load()), time.Duration(h.quotaWaitNanos.Load())
}

// CreateAlbum não é implementado: a API do sxcu.net tem suporte real a
// "collections" (POST /collections/create + upload com collection_token),
// mas o Pipeline nunca chama Host.CreateAlbum hoje — nenhum host implementa
// de verdade (todos são stub). Implementar aqui seria código morto até o
// pipeline realmente agrupar capítulos em álbuns.
func (h *SxcuHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}
