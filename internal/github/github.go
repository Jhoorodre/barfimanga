package github

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"barfimanga/internal/config"
)

type Client struct {
	config  config.Config
	client  *http.Client
	baseURL string // apontável pra um servidor de teste; produção usa api.github.com
}

func NewClient(cfg config.Config) *Client {
	return &Client{
		config:  cfg,
		client:  &http.Client{Timeout: 30 * time.Second},
		baseURL: "https://api.github.com",
	}
}

type FileResponse struct {
	SHA     string `json:"sha"`
	Content string `json:"content"`
}

// UploadJSON atualiza ou cria o arquivo JSON no repositório final do mangá.
// Tenta novamente com backoff exponencial em caso de falha de rede — evita
// perder um lote inteiro de upload de imagens por causa de um blip de
// conexão só no passo final de sincronização com o GitHub.
func (c *Client) UploadJSON(ctx context.Context, filePath string, content []byte, message string) error {
	if c.config.GitHubToken == "" || c.config.GitHubRepo == "" {
		return fmt.Errorf("github_token ou github_repo ausente nas configurações")
	}

	maxRetries := c.config.MaxRetries
	if maxRetries <= 0 {
		maxRetries = 8
	}

	var lastErr error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		lastErr = c.uploadOnce(ctx, filePath, content, message)
		if lastErr == nil {
			return nil
		}
		if attempt < maxRetries {
			backoff := time.Duration(1<<uint(attempt)) * time.Second
			select {
			case <-time.After(backoff):
			case <-ctx.Done():
				return ctx.Err()
			}
		}
	}
	return fmt.Errorf("falha após %d tentativas: %w", maxRetries, lastErr)
}

func (c *Client) uploadOnce(ctx context.Context, filePath string, content []byte, message string) error {
	url := fmt.Sprintf("%s/repos/%s/contents/%s", c.baseURL, c.config.GitHubRepo, filePath)

	// 1. Obtém SHA do arquivo (se ele já existir no Git)
	sha, _ := c.getFileSHA(ctx, url)

	// 2. Prepara o payload convertido para base64
	encoded := base64.StdEncoding.EncodeToString(content)
	payload := map[string]interface{}{
		"message": message,
		"content": encoded,
		"branch":  c.config.GitHubBranch,
	}
	if sha != "" {
		payload["sha"] = sha
	}

	bodyData, _ := json.Marshal(payload)
	req, err := http.NewRequestWithContext(ctx, "PUT", url, bytes.NewReader(bodyData))
	if err != nil {
		return err
	}

	req.Header.Set("Authorization", "token "+c.config.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("erro na api do github (%d): %s", resp.StatusCode, string(b))
	}

	return nil
}

// getFileSHA busca a chave única (SHA) do arquivo para permitir edição no GitHub API.
func (c *Client) getFileSHA(ctx context.Context, url string) (string, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url+"?ref="+c.config.GitHubBranch, nil)
	if err != nil {
		return "", err
	}

	req.Header.Set("Authorization", "token "+c.config.GitHubToken)
	req.Header.Set("Accept", "application/vnd.github.v3+json")

	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("arquivo não encontrado ou erro %d", resp.StatusCode)
	}

	var fileResp FileResponse
	if err := json.NewDecoder(resp.Body).Decode(&fileResp); err != nil {
		return "", err
	}

	return fileResp.SHA, nil
}
