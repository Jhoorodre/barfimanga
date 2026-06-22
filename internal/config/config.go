package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
)

// MangaEntry representa uma obra salva na biblioteca.
type MangaEntry struct {
	Name         string `json:"name"`          // Nome de exibição
	LocalPath    string `json:"local_path"`    // Caminho no PC (Windows ou Linux)
	MetadataPath string `json:"metadata_path"` // Onde salvar JSON e DBs locais
	MangaID      string `json:"manga_id"`      // ID usado no JSON
	GitHubFolder string `json:"github_folder"` // Pasta no GitHub
	Description  string `json:"description"`
	Artist       string `json:"artist"`
	Author       string `json:"author"`
	Cover        string `json:"cover"`
	Status       string `json:"status"`
}

// Config representa as configurações de um perfil individual.
type Config struct {
	GitHubToken  string                `json:"github_token,omitempty"`
	GitHubRepo   string                `json:"github_repo,omitempty"` // format: owner/repo
	GitHubBranch string                `json:"github_branch,omitempty"`
	ScanGroup    string                `json:"scan_group,omitempty"`   // e.g. "Default" or "MyScan"
	DefaultHost  string                `json:"default_host,omitempty"` // e.g. "catbox"
	HostToken    string                `json:"host_token,omitempty"`   // e.g. userhash for catbox
	Workers      int                   `json:"workers,omitempty"`
	RateLimit    float64               `json:"rate_limit,omitempty"`
	Library      map[string]MangaEntry `json:"library,omitempty"`
}

// MultiConfig representa o arquivo de configuração completo com perfis.
type MultiConfig struct {
	ActiveProfile string            `json:"active_profile"`
	Profiles      map[string]Config `json:"profiles"`
}

// GetDefaultConfig retorna as configurações padrão.
func GetDefaultConfig() Config {
	return Config{
		GitHubBranch: "main",
		ScanGroup:    "Default",
		DefaultHost:  "catbox",
		Workers:      5,
		RateLimit:    1.0,
		Library:      make(map[string]MangaEntry),
	}
}

// ConfigDir retorna o diretório onde o arquivo de configuração é armazenado.
func ConfigDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".config", "barfimanga"), nil
}

// LoadConfig carrega as configurações do arquivo JSON.
func LoadConfig() (MultiConfig, error) {
	dir, err := ConfigDir()
	if err != nil {
		return MultiConfig{}, err
	}
	path := filepath.Join(dir, "config.json")

	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			// Cria configuração padrão se não existir
			mCfg := MultiConfig{
				ActiveProfile: "default",
				Profiles: map[string]Config{
					"default": GetDefaultConfig(),
				},
			}
			err = SaveConfig(mCfg)
			return mCfg, err
		}
		return MultiConfig{}, err
	}

	var mCfg MultiConfig
	if err := json.Unmarshal(data, &mCfg); err != nil {
		return MultiConfig{}, err
	}

	// Fallbacks de segurança para evitar pânicos
	if mCfg.Profiles == nil {
		mCfg.Profiles = make(map[string]Config)
	}
	
	// Garante que o map Library exista em todos os perfis
	for name, prof := range mCfg.Profiles {
		if prof.Library == nil {
			prof.Library = make(map[string]MangaEntry)
			mCfg.Profiles[name] = prof
		}
	}

	if mCfg.ActiveProfile == "" {
		mCfg.ActiveProfile = "default"
	}
	if _, ok := mCfg.Profiles[mCfg.ActiveProfile]; !ok {
		mCfg.Profiles[mCfg.ActiveProfile] = GetDefaultConfig()
	}

	return mCfg, nil
}

// SaveConfig salva as configurações no arquivo JSON.
func SaveConfig(mCfg MultiConfig) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}

	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	path := filepath.Join(dir, "config.json")
	data, err := json.MarshalIndent(mCfg, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

// GetActive retorna o perfil de configuração ativo no momento.
func (m *MultiConfig) GetActive() Config {
	if cfg, ok := m.Profiles[m.ActiveProfile]; ok {
		return cfg
	}
	return GetDefaultConfig()
}
