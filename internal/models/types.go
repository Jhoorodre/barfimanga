package models

// UploadResult representa o resultado do upload de um único arquivo.
type UploadResult struct {
	URL      string
	Filename string
	Success  bool
	Error    string
}

// Chapter representa os metadados de um capítulo no reader.json.
type Chapter struct {
	Title       string              `json:"title"`
	Volume      string              `json:"volume"`
	LastUpdated string              `json:"last_updated"`
	Groups      map[string][]string `json:"groups"`
}

// ReaderJSON representa a estrutura principal do arquivo JSON do mangá.
type ReaderJSON struct {
	Title       string             `json:"title"`
	Description string             `json:"description"`
	Artist      string             `json:"artist"`
	Author      string             `json:"author"`
	Cover       string             `json:"cover"`
	Status      string             `json:"status"`
	Chapters    map[string]Chapter `json:"chapters"`
}
