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
	config config.Config
	client *http.Client
}

func NewClient(cfg config.Config) *Client {
	return &Client{
		config: cfg,
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

type FileResponse struct {
	SHA     string `json:"sha"`
	Content string `json:"content"`
}

// UploadJSON atualiza ou cria o arquivo JSON no repositório final do mangá.
func (c *Client) UploadJSON(ctx context.Context, filePath string, content []byte, message string) error {
	if c.config.GitHubToken == "" || c.config.GitHubRepo == "" {
		return fmt.Errorf("github_token ou github_repo ausente nas configurações")
	}

	url := fmt.Sprintf("https://api.github.com/repos/%s/contents/%s", c.config.GitHubRepo, filePath)

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
