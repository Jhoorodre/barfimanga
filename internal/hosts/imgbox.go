package hosts

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image/jpeg"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/cookiejar"
	"os"
	"path/filepath"
	"strings"

	"barfimanga/internal/config"
	"barfimanga/internal/models"

	"golang.org/x/image/webp"
)

type ImgboxHost struct {
	config config.Config
	client *http.Client
}

type imgBoxTokenResp struct {
	Ok            bool   `json:"ok"`
	TokenID       int    `json:"token_id"`
	TokenSecret   string `json:"token_secret"`
	GalleryID     string `json:"gallery_id"`
	GallerySecret string `json:"gallery_secret"`
}

type imgBoxResp struct {
	Files []struct {
		OriginalURL string `json:"original_url"`
		Error       string `json:"error"`
	} `json:"files"`
}

func NewImgboxHost(cfg config.Config) *ImgboxHost {
	jar, _ := cookiejar.New(nil)
	return &ImgboxHost{
		config: cfg,
		client: &http.Client{
			Jar: jar,
		},
	}
}

func (h *ImgboxHost) Name() string {
	return "ImgBox"
}

func (h *ImgboxHost) fetchAjaxToken(ctx context.Context) (*imgBoxTokenResp, error) {
	req, _ := http.NewRequestWithContext(ctx, "GET", "https://imgbox.com/ajax/token/generate", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://imgbox.com/")
	req.Header.Set("Accept", "application/json, text/plain, */*")

	resp, err := h.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)

	var rep imgBoxTokenResp
	if err := json.Unmarshal(body, &rep); err != nil {
		return nil, err
	}
	if !rep.Ok {
		return nil, fmt.Errorf("imgbox token error: %s", string(body))
	}

	return &rep, nil
}

func (h *ImgboxHost) convertWebpToJpeg(filePath string) (string, error) {
	if strings.ToLower(filepath.Ext(filePath)) != ".webp" {
		return filePath, nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	img, err := webp.Decode(file)
	if err != nil {
		return "", fmt.Errorf("erro decodificando WebP: %v", err)
	}

	tempFile, err := os.CreateTemp("", "imgbox_*.jpg")
	if err != nil {
		return "", err
	}
	defer tempFile.Close()

	err = jpeg.Encode(tempFile, img, &jpeg.Options{Quality: 95})
	if err != nil {
		return "", fmt.Errorf("erro codificando JPEG: %v", err)
	}

	return tempFile.Name(), nil
}

func (h *ImgboxHost) UploadImage(ctx context.Context, filePath string) (models.UploadResult, error) {
	// 1. Converter WebP se necessário
	uploadPath, err := h.convertWebpToJpeg(filePath)
	if err != nil {
		return models.UploadResult{Filename: filepath.Base(filePath), Success: false, Error: err.Error()}, err
	}
	if uploadPath != filePath {
		defer os.Remove(uploadPath)
	}

	// 2. Pegar Token via AJAX
	token, err := h.fetchAjaxToken(ctx)
	if err != nil {
		return models.UploadResult{Filename: filepath.Base(filePath), Success: false, Error: "falha token: " + err.Error()}, err
	}

	// 3. Preparar Multipart
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("token_id", fmt.Sprintf("%d", token.TokenID))
	_ = writer.WriteField("token_secret", token.TokenSecret)
	_ = writer.WriteField("content_type", "1")
	_ = writer.WriteField("thumbnail_size", "100c")
	_ = writer.WriteField("comments_enabled", "1")
	_ = writer.WriteField("gallery_id", "null")
	_ = writer.WriteField("gallery_secret", "null")

	file, err := os.Open(uploadPath)
	if err != nil {
		return models.UploadResult{Filename: filepath.Base(filePath), Success: false, Error: err.Error()}, err
	}
	defer file.Close()

	mh := make(map[string][]string)
	mh["Content-Disposition"] = []string{fmt.Sprintf(`form-data; name="files[]"; filename="%s"`, filepath.Base(uploadPath))}
	mh["Content-Type"] = []string{"image/jpeg"}

	part, err := writer.CreatePart(mh)
	if err != nil {
		return models.UploadResult{Filename: filepath.Base(filePath), Success: false, Error: "erro multipart: " + err.Error()}, err
	}
	_, _ = io.Copy(part, file)
	_ = writer.Close()

	// 4. Enviar Upload
	req, _ := http.NewRequestWithContext(ctx, "POST", "https://imgbox.com/upload/process", body)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("Referer", "https://imgbox.com/")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")

	resp, err := h.client.Do(req)
	if err != nil {
		return models.UploadResult{Filename: filepath.Base(filePath), Success: false, Error: err.Error()}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return models.UploadResult{Filename: filepath.Base(filePath), Success: false, Error: fmt.Sprintf("status erro: %d", resp.StatusCode)}, fmt.Errorf("erro status: %d", resp.StatusCode)
	}

	var result imgBoxResp
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return models.UploadResult{Filename: filepath.Base(filePath), Success: false, Error: "erro parse json"}, err
	}

	if len(result.Files) == 0 || result.Files[0].OriginalURL == "" {
		errMsg := "erro desconhecido no imgbox"
		if len(result.Files) > 0 && result.Files[0].Error != "" {
			errMsg = result.Files[0].Error
		}
		return models.UploadResult{Filename: filepath.Base(filePath), Success: false, Error: errMsg}, fmt.Errorf("%s", errMsg)
	}

	return models.UploadResult{
		Filename: filepath.Base(filePath),
		URL:      result.Files[0].OriginalURL,
		Success:  true,
	}, nil
}

func (h *ImgboxHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}
