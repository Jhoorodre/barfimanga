package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
)

// MangaEntry representa uma obra salva na biblioteca.
type MangaEntry struct {
	Name             string `json:"name"`
	LocalPath        string `json:"local_path"`
	MetadataPath     string `json:"metadata_path"`
	AutoMoveMetadata bool   `json:"auto_move_metadata"`
	MangaID          string `json:"manga_id"`
	GitHubFolder     string `json:"github_folder"`
	Description      string `json:"description"`
	Artist           string `json:"artist"`
	Author           string `json:"author"`
	Cover            string `json:"cover"`
	Status           string `json:"status"`
	ScanGroup        string `json:"scan_group,omitempty"`
	SakuraMangasDB   string `json:"sakura_db,omitempty"`
}

// Config representa as configurações de um perfil individual.
type Config struct {
	GitHubTokenEnv string                `json:"github_token_env,omitempty"` // Qual var do .env usar
	GitHubToken    string                `json:"-"`                          // Mantido em memória (lido dinamicamente do .env)
	GitHubRepo     string                `json:"github_repo,omitempty"`      // owner/repo
	GitHubBranch   string                `json:"github_branch,omitempty"`
	ScanGroup      string                `json:"scan_group,omitempty"`
	DefaultHost    string                `json:"default_host,omitempty"`
	HostToken      string                `json:"host_token,omitempty"`
	Workers        int                   `json:"workers,omitempty"`
	RateLimit      float64               `json:"rate_limit,omitempty"`
	MaxRetries     int                   `json:"max_retries,omitempty"`
	Library        map[string]MangaEntry `json:"-"` // Mantido na memória (mas salvo no bd/library.json)
}

// MultiConfig representa o arquivo de configuração completo com perfis (memory).
type MultiConfig struct {
	ActiveProfile string            `json:"active_profile"`
	Profiles      map[string]Config `json:"profiles"`
}

// LibraryData é o modelo para salvar separadamente as obras por perfil no bd/library.json
type LibraryData struct {
	Profiles map[string]map[string]MangaEntry `json:"profiles"`
}

func GetDefaultConfig() Config {
	return Config{
		GitHubBranch:   "main",
		ScanGroup:      "Default",
		DefaultHost:    "catbox",
		Workers:        5,
		RateLimit:      1.0,
		MaxRetries:     8,
		GitHubTokenEnv: "PAT_DEFAULT", // Por padrão procura no .env
		Library:        make(map[string]MangaEntry),
	}
}

func ConfigDir() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", err
	}
	return filepath.Join(cwd, "bd"), nil
}

func LoadConfig() (MultiConfig, error) {
	// Carrega .env se existir, ignora erro se não existir
	_ = godotenv.Load()

	dir, err := ConfigDir()
	if err != nil {
		return MultiConfig{}, err
	}

	// Garante que a pasta bd/ exista
	os.MkdirAll(dir, 0700)

	profilesPath := filepath.Join(dir, "profiles.json")
	libraryPath := filepath.Join(dir, "library.json")

	var mCfg MultiConfig
	dataProfiles, err := os.ReadFile(profilesPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			mCfg = MultiConfig{
				ActiveProfile: "default",
				Profiles: map[string]Config{
					"default": GetDefaultConfig(),
				},
			}
			SaveConfig(mCfg)
		} else {
			return MultiConfig{}, err
		}
	} else {
		if err := json.Unmarshal(dataProfiles, &mCfg); err != nil {
			return MultiConfig{}, err
		}
	}

	if mCfg.Profiles == nil {
		mCfg.Profiles = make(map[string]Config)
	}

	var libData LibraryData
	dataLibrary, err := os.ReadFile(libraryPath)
	if err == nil {
		_ = json.Unmarshal(dataLibrary, &libData)
	}
	if libData.Profiles == nil {
		libData.Profiles = make(map[string]map[string]MangaEntry)
	}

	for name, prof := range mCfg.Profiles {
		// Puxa a biblioteca do library.json e anexa ao perfil em memória
		if lib, ok := libData.Profiles[name]; ok {
			prof.Library = lib
		} else {
			prof.Library = make(map[string]MangaEntry)
		}

		// Puxa o PAT do .env baseado na string referenciada no JSON
		if prof.GitHubTokenEnv != "" {
			if pat, err := DecryptToken(os.Getenv(prof.GitHubTokenEnv)); err == nil {
				prof.GitHubToken = pat
			} else {
				fmt.Fprintf(os.Stderr, "barfimanga: aviso: PAT do perfil %q não pôde ser descriptografado (mudou de máquina?) — reconfigure em Gerenciar Perfis\n", name)
			}
		}

		if dec, err := DecryptToken(prof.HostToken); err == nil {
			prof.HostToken = dec
		} else {
			fmt.Fprintf(os.Stderr, "barfimanga: aviso: host_token do perfil %q não pôde ser descriptografado (mudou de máquina?) — reconfigure em Gerenciar Perfis\n", name)
			prof.HostToken = ""
		}

		mCfg.Profiles[name] = prof
	}

	if mCfg.ActiveProfile == "" {
		mCfg.ActiveProfile = "default"
	}
	if _, ok := mCfg.Profiles[mCfg.ActiveProfile]; !ok {
		mCfg.Profiles[mCfg.ActiveProfile] = GetDefaultConfig()
	}

	return mCfg, nil
}

func SaveConfig(mCfg MultiConfig) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	os.MkdirAll(dir, 0700)

	// 1. Extrair Library para salvar no library.json e cifrar HostToken numa
	// cópia (mCfg em memória continua com o token em texto puro pro resto da sessão)
	libData := LibraryData{
		Profiles: make(map[string]map[string]MangaEntry),
	}
	safe := MultiConfig{ActiveProfile: mCfg.ActiveProfile, Profiles: make(map[string]Config, len(mCfg.Profiles))}
	for name, prof := range mCfg.Profiles {
		libData.Profiles[name] = prof.Library
		if enc, err := EncryptToken(prof.HostToken); err == nil {
			prof.HostToken = enc
		}
		safe.Profiles[name] = prof
	}

	// 2. Salva profiles.json (O struct ignora GitHubToken e Library automaticamente)
	pathProfiles := filepath.Join(dir, "profiles.json")
	pData, err := json.MarshalIndent(safe, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(pathProfiles, pData, 0600); err != nil {
		return err
	}

	// 3. Salva library.json
	pathLibrary := filepath.Join(dir, "library.json")
	lData, err := json.MarshalIndent(libData, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pathLibrary, lData, 0600)
}

func (m *MultiConfig) GetActive() Config {
	if cfg, ok := m.Profiles[m.ActiveProfile]; ok {
		return cfg
	}
	return GetDefaultConfig()
}
