package utils

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// UploadState mantém o registro de capítulos já totalmente processados.
type UploadState struct {
	CompletedChapters map[string]bool `json:"completed_chapters"`
}

// LoadState carrega o arquivo de estado de um diretório de mangá.
func LoadState(dir string) *UploadState {
	path := filepath.Join(dir, ".upload_state.json")
	data, err := os.ReadFile(path)

	state := &UploadState{
		CompletedChapters: make(map[string]bool),
	}

	if err == nil {
		_ = json.Unmarshal(data, state)
	}

	if state.CompletedChapters == nil {
		state.CompletedChapters = make(map[string]bool)
	}

	return state
}

// SaveState salva o estado de progresso no disco.
func SaveState(dir string, state *UploadState) error {
	path := filepath.Join(dir, ".upload_state.json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0644)
}
