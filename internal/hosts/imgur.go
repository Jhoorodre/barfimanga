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

// imgurResponse cobre os dois formatos de erro que a API já devolveu:
// o clássico {success, data:{error}} e o novo estilo JSON:API {errors:[...]}
// que o gateway atual usa (confirmado batendo direto na API — um erro real
// nesse formato novo virava mensagem vazia, já que só líamos data.error).
type imgurResponse struct {
	Success bool `json:"success"`
	Data    struct {
		Link  string `json:"link"`
		Error string `json:"error"`
	} `json:"data"`
	Errors []struct {
		Code   string `json:"code"`
		Detail string `json:"detail"`
	} `json:"errors"`
}

// UploadImage faz upload pro Imgur. Uma tentativa só — retry e backoff já
// são responsabilidade do worker.Pool (que envolve todo host igual).
func (h *ImgurHost) UploadImage(ctx context.Context, fpath string) (models.UploadResult, error) {
	if h.config.HostToken == "" {
		return models.UploadResult{}, fmt.Errorf("host_token (Client-ID) não configurado para Imgur")
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
	req.Header.Set("Authorization", "Client-ID "+h.config.HostToken)

	resp, err := h.client.Do(req)
	if err != nil {
		return models.UploadResult{}, err
	}
	defer resp.Body.Close()

	var result imgurResponse
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return models.UploadResult{}, fmt.Errorf("erro http %d, resposta ilegível: %w", resp.StatusCode, err)
	}

	if !result.Success {
		if len(result.Errors) > 0 {
			return models.UploadResult{}, fmt.Errorf("imgur api error (%d, %s): %s", resp.StatusCode, result.Errors[0].Code, result.Errors[0].Detail)
		}
		return models.UploadResult{}, fmt.Errorf("imgur api error (%d): %s", resp.StatusCode, result.Data.Error)
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
