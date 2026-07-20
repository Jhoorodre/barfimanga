package hosts

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"barfimanga/internal/config"
	"barfimanga/internal/models"
)

// GDriveHost gera links lh3.googleusercontent.com para arquivos já presentes no Google Drive.
// Não faz upload — apenas localiza arquivos existentes na árvore de pastas do Drive.
//
// Auth: Application Default Credentials (ADC).
// Configure com: gcloud auth application-default login
// HostToken: caminho para o arquivo ADC (padrão: ~/.config/gcloud/application_default_credentials.json)
type GDriveHost struct {
	credPath string
	client   *http.Client

	mu          sync.Mutex
	token       string
	tokenExp    time.Time
	creds       *adcCreds
	folderCache map[string]string // "parentID/name" → Drive ID
}

type adcCreds struct {
	ClientID     string `json:"client_id"`
	ClientSecret string `json:"client_secret"`
	RefreshToken string `json:"refresh_token"`
}

func NewGDriveHost(cfg config.Config) *GDriveHost {
	credPath := cfg.HostToken
	if credPath == "" {
		credPath = defaultADCPath()
	}
	return &GDriveHost{
		credPath:    credPath,
		client:      &http.Client{Timeout: 30 * time.Second},
		folderCache: make(map[string]string),
	}
}

// defaultADCPath replica onde o `gcloud` guarda as Application Default Credentials
// em cada SO: %APPDATA%\gcloud\... no Windows, ~/.config/gcloud/... em Linux/Mac.
func defaultADCPath() string {
	if runtime.GOOS == "windows" {
		if appData := os.Getenv("APPDATA"); appData != "" {
			return filepath.Join(appData, "gcloud", "application_default_credentials.json")
		}
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config", "gcloud", "application_default_credentials.json")
}

func (h *GDriveHost) Name() string { return "Google Drive" }

func (h *GDriveHost) loadCreds() error {
	if h.creds != nil {
		return nil
	}
	data, err := os.ReadFile(h.credPath)
	if err != nil {
		return fmt.Errorf("gdrive: lendo ADC %q: %w\n  Dica: rode 'gcloud auth application-default login'", h.credPath, err)
	}
	var c adcCreds
	if err := json.Unmarshal(data, &c); err != nil {
		return fmt.Errorf("gdrive: parse ADC: %w", err)
	}
	if c.RefreshToken == "" {
		return fmt.Errorf("gdrive: ADC sem refresh_token — rode: gcloud auth application-default login")
	}
	h.creds = &c
	return nil
}

func (h *GDriveHost) accessToken(ctx context.Context) (string, error) {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.token != "" && time.Now().Before(h.tokenExp.Add(-30*time.Second)) {
		return h.token, nil
	}
	if err := h.loadCreds(); err != nil {
		return "", err
	}

	resp, err := h.client.PostForm("https://oauth2.googleapis.com/token", url.Values{
		"grant_type":    {"refresh_token"},
		"client_id":     {h.creds.ClientID},
		"client_secret": {h.creds.ClientSecret},
		"refresh_token": {h.creds.RefreshToken},
	})
	if err != nil {
		return "", fmt.Errorf("gdrive: refresh token: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gdrive: token erro %d: %s", resp.StatusCode, body)
	}

	var tr struct {
		AccessToken string `json:"access_token"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(body, &tr); err != nil || tr.AccessToken == "" {
		return "", fmt.Errorf("gdrive: parse token: %w", err)
	}
	h.token = tr.AccessToken
	h.tokenExp = time.Now().Add(time.Duration(tr.ExpiresIn) * time.Second)
	return h.token, nil
}

// parseDrivePath extrai o folder ID raiz e as partes do caminho relativo.
// Suporta o padrão do Drive desktop: .shortcut-targets-by-id/{folderID}/...
func parseDrivePath(fpath string) (rootID string, parts []string, err error) {
	normalized := filepath.ToSlash(fpath)
	const marker = ".shortcut-targets-by-id/"
	idx := strings.Index(normalized, marker)
	if idx == -1 {
		return "", nil, fmt.Errorf("gdrive: caminho sem padrão .shortcut-targets-by-id: %s\n  Configure LocalPath apontando para a pasta do Drive", fpath)
	}
	after := normalized[idx+len(marker):]
	slash := strings.Index(after, "/")
	if slash == -1 {
		return "", nil, fmt.Errorf("gdrive: caminho incompleto (só folder ID, sem arquivo): %s", fpath)
	}
	rootID = after[:slash]
	rel := after[slash+1:]
	for _, p := range strings.Split(rel, "/") {
		if p != "" {
			parts = append(parts, p)
		}
	}
	return rootID, parts, nil
}

// findInDrive retorna o Drive ID do item com o nome dado dentro de parentID.
// Resultados são cacheados para evitar chamadas repetidas na mesma sessão.
func (h *GDriveHost) findInDrive(ctx context.Context, tok, parentID, name string) (string, error) {
	cacheKey := parentID + "/" + name
	h.mu.Lock()
	if id, ok := h.folderCache[cacheKey]; ok {
		h.mu.Unlock()
		return id, nil
	}
	h.mu.Unlock()

	escaped := strings.ReplaceAll(name, "'", "\\'")
	q := fmt.Sprintf("'%s' in parents and name='%s' and trashed=false", parentID, escaped)
	apiURL := "https://www.googleapis.com/drive/v3/files?q=" +
		url.QueryEscape(q) + "&fields=files(id)&pageSize=5"

	req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("Authorization", "Bearer "+tok)

	resp, err := h.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("gdrive list: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("gdrive list erro %d: %s", resp.StatusCode, body)
	}

	var result struct {
		Files []struct {
			ID string `json:"id"`
		} `json:"files"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("gdrive: parse list: %w", err)
	}
	if len(result.Files) == 0 {
		return "", fmt.Errorf("gdrive: '%s' não encontrado em '%s'", name, parentID)
	}

	id := result.Files[0].ID
	h.mu.Lock()
	h.folderCache[cacheKey] = id
	h.mu.Unlock()
	return id, nil
}

// UploadImage localiza o arquivo no Google Drive e retorna seu link lh3.
// O arquivo deve já existir no Drive (sincronizado via Drive desktop).
func (h *GDriveHost) UploadImage(ctx context.Context, fpath string) (models.UploadResult, error) {
	tok, err := h.accessToken(ctx)
	if err != nil {
		return models.UploadResult{Filename: filepath.Base(fpath), Success: false, Error: err.Error()}, err
	}

	rootID, parts, err := parseDrivePath(fpath)
	if err != nil {
		return models.UploadResult{Filename: filepath.Base(fpath), Success: false, Error: err.Error()}, err
	}

	currentID := rootID
	for _, part := range parts {
		id, err := h.findInDrive(ctx, tok, currentID, part)
		if err != nil {
			return models.UploadResult{
				Filename: filepath.Base(fpath),
				Success:  false,
				Error:    err.Error(),
			}, err
		}
		currentID = id
	}

	lh3URL := "https://lh3.googleusercontent.com/d/" + currentID + "=s0"
	return models.UploadResult{URL: lh3URL, Filename: filepath.Base(fpath), Success: true}, nil
}

func (h *GDriveHost) CreateAlbum(_ context.Context, _, _ string, _ []string) (string, error) {
	return "", nil
}
