package utils

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"barfimanga/internal/models"
)

// LoadJSON tenta carregar um reader.json do disco.
func LoadJSON(path string) (*models.ReaderJSON, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var r models.ReaderJSON
	if err := json.Unmarshal(data, &r); err != nil {
		return nil, err
	}
	return &r, nil
}

// SaveJSON salva a estrutura ReaderJSON em disco com segurança (incremental), forçando ordem decrescente.
func SaveJSON(path string, data *models.ReaderJSON) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return err
	}

	// Extrai as chaves dos capítulos
	keys := make([]string, 0, len(data.Chapters))
	for k := range data.Chapters {
		keys = append(keys, k)
	}

	// Ordena as chaves de forma decrescente numérica, suportando decimais ("019.1", "019.2")
	sort.Slice(keys, func(i, j int) bool {
		numI, _ := strconv.ParseFloat(keys[i], 64)
		numJ, _ := strconv.ParseFloat(keys[j], 64)
		return numI > numJ
	})

	buffer := &bytes.Buffer{}
	buffer.WriteString("{\n")

	// Serializa os campos base do arquivo JSON
	baseData := map[string]string{
		"title":       data.Title,
		"description": data.Description,
		"artist":      data.Artist,
		"author":      data.Author,
		"cover":       data.Cover,
		"status":      data.Status,
	}

	// Printa os meta-dados basicos primeiro
	metaKeys := []string{"title", "description", "artist", "author", "cover", "status"}
	for _, mk := range metaKeys {
		valJSON, _ := json.Marshal(baseData[mk])
		buffer.WriteString(fmt.Sprintf("  \"%s\": %s,\n", mk, string(valJSON)))
	}

	buffer.WriteString("  \"chapters\": {\n")

	// Itera na ordem correta e insere
	for i, key := range keys {
		chapterJSON, _ := json.MarshalIndent(data.Chapters[key], "    ", "  ")
		buffer.WriteString(fmt.Sprintf("    \"%s\": %s", key, string(chapterJSON)))
		if i < len(keys)-1 {
			buffer.WriteString(",\n")
		} else {
			buffer.WriteString("\n")
		}
	}

	buffer.WriteString("  }\n}")

	return os.WriteFile(path, buffer.Bytes(), 0644)
}

// normTitle extrai o subtítulo após " - " para comparação independente de prefixo (Ch.xxx vs Cap xxx).
func normTitle(t string) string {
	if idx := strings.Index(t, " - "); idx >= 0 {
		return strings.TrimSpace(t[idx+3:])
	}
	return strings.TrimSpace(t)
}

// findChapterByTitle verifica se um capítulo com o mesmo subtítulo já existe.
func findChapterByTitle(chapters map[string]models.Chapter, title string) string {
	norm := normTitle(title)
	for k, v := range chapters {
		if normTitle(v.Title) == norm {
			return k
		}
	}
	return ""
}

// MergeMetadata mescla o JSON atualizado com o existente usando lógicas de add, replace ou smart.
func MergeMetadata(existing *models.ReaderJSON, newData *models.ReaderJSON, mode string) *models.ReaderJSON {
	merged := &models.ReaderJSON{
		Title:       existing.Title,
		Description: existing.Description,
		Artist:      existing.Artist,
		Author:      existing.Author,
		Cover:       existing.Cover,
		Status:      existing.Status,
		Chapters:    make(map[string]models.Chapter),
	}

	// Atualiza as chaves base se não forem vazias no novo
	if newData.Title != "" {
		merged.Title = newData.Title
	}
	if newData.Description != "" {
		merged.Description = newData.Description
	}
	if newData.Artist != "" {
		merged.Artist = newData.Artist
	}
	if newData.Author != "" {
		merged.Author = newData.Author
	}
	if newData.Cover != "" {
		merged.Cover = newData.Cover
	}
	if newData.Status != "" {
		merged.Status = newData.Status
	}

	// Copia chapters existentes
	for k, v := range existing.Chapters {
		merged.Chapters[k] = v
	}

	if mode == "replace" {
		merged.Chapters = newData.Chapters
		return merged
	}

	// Adiciona ou faz update (smart/add mode)
	// Para garantir que a ordem das chaves (001, 002, 003) coincida com a ordem cronológica dos capítulos,
	// vamos ordenar os novos capítulos antes de atribuir chaves.

	var newChTitles []string
	for title := range newData.Chapters {
		newChTitles = append(newChTitles, title)
	}

	// Ordena os novos capítulos de forma CRESCENTE para que o mais antigo ganhe o menor ID disponível
	// e o mais novo ganhe o maior ID.
	sort.Slice(newChTitles, func(i, j int) bool {
		// Usamos a mesma lógica de NaturalSort mas de forma ascendente aqui
		a := extractNumbersAsc(newChTitles[i])
		b := extractNumbersAsc(newChTitles[j])
		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				numA, errA := strconv.Atoi(a[k])
				numB, errB := strconv.Atoi(b[k])
				if errA == nil && errB == nil {
					return numA < numB
				}
				return strings.ToLower(a[k]) < strings.ToLower(b[k])
			}
		}
		return len(a) < len(b)
	})

	for _, key := range newChTitles {
		newCh := newData.Chapters[key]
		// Remove TODAS as entradas stale com mesmo subtítulo mas chave diferente
		for {
			existingKey := findChapterByTitle(merged.Chapters, newCh.Title)
			if existingKey == "" || existingKey == key {
				break
			}
			delete(merged.Chapters, existingKey)
		}
		merged.Chapters[key] = newCh
	}

	return merged
}

func extractNumbersAsc(s string) []string {
	// Reutiliza a lógica mas mantém aqui para evitar dependência circular se necessário
	// ou apenas para clareza local.
	return strings.FieldsFunc(s, func(r rune) bool {
		return !('0' <= r && r <= '9') && !('a' <= r && r <= 'z') && !('A' <= r && r <= 'Z')
	})
}
