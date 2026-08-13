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

type ImgChestHost struct {
	config config.Config
	client *http.Client
}

func NewImgChestHost(cfg config.Config) *ImgChestHost {
	return &ImgChestHost{
		config: cfg,
		client: &http.Client{Timeout: 5 * time.Minute},
	}
}

func (h *ImgChestHost) Name() string {
	return "ImgChest"
}

// imgChestPostResponse é o formato de "criar post" da API v1 (POST /post):
// https://imgchest.com/docs/api/1.0/endpoints/post
type imgChestPostResponse struct {
	Data struct {
		Images []struct {
			Link string `json:"link"`
		} `json:"images"`
	} `json:"data"`
}

// imgChestRateLimitResponse cobre o corpo do 429 de cota de upload
// ("Upload limit reached. Try again in N minutes."), que vem com
// retry_after em segundos — diferente do 429 genérico de rate-limit de
// requisição (throttle do Laravel, só {"message":"Too Many Attempts."}).
type imgChestRateLimitResponse struct {
	RetryAfter int `json:"retry_after"`
}

// maxRetryAfterWait limita quanto tempo esperamos mesmo que a API peça mais
// (proteção contra um valor absurdo/malformado — não é o cenário observado,
// mas retry_after vem de fora, então validamos).
const maxRetryAfterWait = 2 * time.Hour

// UploadImage sobe UM arquivo como um post novo no ImgChest (a API não tem
// endpoint de "upload avulso" — todo arquivo vira um post, mesmo que com uma
// imagem só). Uma tentativa só (retry entre imagens fica a cargo do
// worker.Pool) — mas se bater na cota de upload (429 com retry_after), dorme
// o tempo exato que a API pediu antes de devolver o erro. Sem isso, o pool
// ficaria martelando a API a cada poucos segundos por ~1h até a cota voltar,
// sem nenhuma chance de dar certo nesse meio tempo.
func (h *ImgChestHost) UploadImage(ctx context.Context, fpath string) (models.UploadResult, error) {
	if h.config.HostToken == "" {
		return models.UploadResult{}, fmt.Errorf("host_token (Personal Access Token) não configurado para ImgChest")
	}

	file, err := os.Open(fpath)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer file.Close()

	var b bytes.Buffer
	w := multipart.NewWriter(&b)

	fw, err := w.CreateFormFile("images[]", filepath.Base(fpath))
	if err != nil {
		return models.UploadResult{}, err
	}
	if _, err = io.Copy(fw, file); err != nil {
		return models.UploadResult{}, err
	}
	if err = w.Close(); err != nil {
		return models.UploadResult{}, err
	}

	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.imgchest.com/v1/post", &b)
	if err != nil {
		return models.UploadResult{}, err
	}
	req.Header.Set("Content-Type", w.FormDataContentType())
	req.Header.Set("Authorization", "Bearer "+h.config.HostToken)
	req.Header.Set("Accept", "application/json")

	resp, err := h.client.Do(req)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer resp.Body.Close()

	bodyBytes, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		if resp.StatusCode == http.StatusTooManyRequests {
			waitForRetryAfter(ctx, bodyBytes)
		}
		return models.UploadResult{}, fmt.Errorf("erro http %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var result imgChestPostResponse
	if err := json.Unmarshal(bodyBytes, &result); err != nil {
		return models.UploadResult{}, err
	}

	if len(result.Data.Images) == 0 || result.Data.Images[0].Link == "" {
		return models.UploadResult{}, fmt.Errorf("resposta do imgchest sem link de imagem: %s", string(bodyBytes))
	}

	return models.UploadResult{
		URL:      result.Data.Images[0].Link,
		Filename: filepath.Base(fpath),
		Success:  true,
	}, nil
}

// waitForRetryAfter dorme retry_after segundos (se o corpo do 429 trouxer
// esse campo) antes de devolver o controle. Ignora silenciosamente se o
// corpo não tiver retry_after (ex: o 429 genérico "Too Many Attempts." do
// throttle do Laravel) — nesse caso o backoff normal do worker.Pool decide.
func waitForRetryAfter(ctx context.Context, body []byte) {
	var rl imgChestRateLimitResponse
	if err := json.Unmarshal(body, &rl); err != nil || rl.RetryAfter <= 0 {
		return
	}
	wait := time.Duration(rl.RetryAfter) * time.Second
	if wait > maxRetryAfterWait {
		wait = maxRetryAfterWait
	}
	select {
	case <-time.After(wait):
	case <-ctx.Done():
	}
}

func (h *ImgChestHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}
