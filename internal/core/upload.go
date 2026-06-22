package core

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"barfimanga/internal/cache"
	"barfimanga/internal/config"
	"barfimanga/internal/github"
	"barfimanga/internal/hosts"
	"barfimanga/internal/models"
	"barfimanga/internal/progress"
	"barfimanga/internal/utils"
	"barfimanga/internal/worker"
)

// Pipeline orquestra a lógica principal de scanning, upload e envio ao github.
type Pipeline struct {
	mCfg   config.MultiConfig
	active config.Config
	host   hosts.Host
	client *github.Client
}

func NewPipeline(mCfg config.MultiConfig) (*Pipeline, error) {
	active := mCfg.GetActive()

	var h hosts.Host
	switch strings.ToLower(active.DefaultHost) {
	case "catbox", "":
		h = hosts.NewCatboxHost(active)
	case "imgur":
		h = hosts.NewImgurHost(active)
	case "imgbb":
		h = hosts.NewImgBBHost(active)
	case "imagechest":
		h = hosts.NewImageChestHost(active)
	case "imghippo":
		h = hosts.NewImgHippoHost(active)
	case "lensdump":
		h = hosts.NewLensdumpHost(active)
	case "pixeldrain":
		h = hosts.NewPixeldrainHost(active)
	case "imgpile":
		h = hosts.NewImgPileHost(active)
	case "gofile":
		h = hosts.NewGofileHost(active)
	case "imgbox":
		h = hosts.NewImgboxHost(active)
	default:
		return nil, fmt.Errorf("host '%s' não suportado", active.DefaultHost)
	}

	return &Pipeline{
		mCfg:   mCfg,
		active: active,
		host:   h,
		client: github.NewClient(active),
	}, nil
}

func (p *Pipeline) Run(ctx context.Context, dir string, quiet bool, groupName string, mangaID string, ghFolder string, useRoot bool, forceRebuild bool, entry config.MangaEntry, syncOnly bool) error {
	mangaTitle := filepath.Base(dir)
	mangaRoot := dir

	// 2. Escaneia sub-diretórios (capítulos) - Pula se for apenas Sync
	var chapters []string
	hasSubDirs := false
	if !syncOnly {
		if !quiet {
			fmt.Printf(">> Analisando diretório: %s\n", dir)
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}

		for _, e := range entries {
			if e.IsDir() {
				chapters = append(chapters, e.Name())
				hasSubDirs = true
			}
		}

		// [Auto-fill / Single Chapter Mode]
		if !hasSubDirs {
			images, _ := p.findImages(dir)
			if len(images) > 0 {
				if !quiet {
					fmt.Println(">> [Auto-fill] Pasta de capítulo único detectada. Usando diretório pai como raiz da obra.")
				}
				mangaRoot = filepath.Dir(dir)
				mangaTitle = filepath.Base(mangaRoot)
				chapters = []string{filepath.Base(dir)}
			}
		}

		if len(chapters) == 0 {
			return fmt.Errorf("nenhuma pasta de capítulo encontrada")
		}
	}

	// Define o identificador da obra (ID se fornecido, senão nome da pasta raiz sanitizado)
	effectiveID := mangaID
	if effectiveID == "" {
		effectiveID = utils.SanitizeFilename(mangaTitle, false)
	}

	// Define onde os DBs e o JSON serão salvos
	dbRoot := mangaRoot
	if entry.MetadataPath != "" {
		// Converte caminho Windows para WSL e adiciona subpasta com o Nome da Obra (mangaTitle)
		safeDirName := utils.SanitizeFilename(mangaTitle, false)
		dbRoot = filepath.Join(utils.ToWSLPath(entry.MetadataPath), safeDirName)
		if err := os.MkdirAll(dbRoot, 0755); err != nil {
			return fmt.Errorf("erro ao criar diretório de metadados: %v", err)
		}
	}
	
	jsonFilename := effectiveID + ".json"
	jsonPath := filepath.Join(dbRoot, jsonFilename)

	// 1. Carrega JSON existente ou cria base em branco
	existingJson, err := utils.LoadJSON(jsonPath)
	if err != nil {
		if !quiet {
			fmt.Printf(">> Novo mangá detectado (%s). Inicializando arquivo de metadata.\n", jsonFilename)
		}
		existingJson = &models.ReaderJSON{
			Title:    mangaTitle,
			Chapters: make(map[string]models.Chapter),
		}
	}

	// Se for APENAS Sync, atualiza metadados do cabeçalho e sobe pro GitHub
	if syncOnly {
		if !quiet {
			fmt.Printf(">> [Fast-Sync] Atualizando apenas metadados de '%s'...\n", mangaTitle)
		}
		existingJson.Title = mangaTitle
		existingJson.Description = entry.Description
		existingJson.Artist = entry.Artist
		existingJson.Author = entry.Author
		existingJson.Cover = entry.Cover
		existingJson.Status = entry.Status

		if err := utils.SaveJSON(jsonPath, existingJson); err != nil {
			return fmt.Errorf("erro salvando JSON atualizado: %v", err)
		}
		return p.uploadToGitHub(ctx, jsonPath, jsonFilename, effectiveID, ghFolder, useRoot, mangaTitle, quiet)
	}

	// [O resto do processo normal de upload...]
	utils.NaturalSort(chapters)

	newData := &models.ReaderJSON{
		Title:       mangaTitle,
		Description: entry.Description,
		Artist:      entry.Artist,
		Author:      entry.Author,
		Cover:       entry.Cover,
		Status:      entry.Status,
		Chapters:    make(map[string]models.Chapter),
	}

	uploadCache := cache.NewCache(dbRoot)
	defer uploadCache.Save()

	state := utils.LoadState(dbRoot)

	pool := worker.NewPool(p.host, p.active.Workers, p.active.RateLimit)

	for _, ch := range chapters {
		if state.CompletedChapters[ch] && !forceRebuild {
			if !quiet {
				fmt.Printf("\n-> Capítulo: %s (Pulado via State Checkpoint)\n", ch)
			}
			continue
		}

		if !quiet {
			fmt.Printf("\n-> Capítulo: %s\n", ch)
		}
		
		var chDir string
		if !hasSubDirs {
			chDir = dir
		} else {
			chDir = filepath.Join(dir, ch)
		}

		images, err := p.findImages(chDir)
		if err != nil || len(images) == 0 {
			if !quiet {
				fmt.Printf("   Nenhuma imagem suportada encontrada, ignorando.\n")
			}
			continue
		}

		utils.NaturalSort(images)
		if !quiet {
			fmt.Printf("   Iniciando upload de %d imagens... (Host: %s, Workers: %d)\n", len(images), p.host.Name(), p.active.Workers)
		}

		tracker := &progress.ProgressTracker{Total: int64(len(images))}
		progUI := progress.NewProgress(quiet)
		progUI.Start(int64(len(images)), tracker)

		results, err := pool.ProcessImages(ctx, images, tracker, uploadCache, forceRebuild)
		progUI.Finish(err == nil)

		if err != nil {
			fmt.Fprintf(os.Stderr, "   [X] Erro crítico processando %s: %v\n", ch, err)
			continue
		}

		var urls []string
		for _, res := range results {
			if res.Success {
				urls = append(urls, res.URL)
			} else {
				fmt.Fprintf(os.Stderr, "   [!] Falha isolada (%s): %s\n", res.Filename, res.Error)
			}
		}

		if len(urls) > 0 {
			if !quiet {
				fmt.Printf("   -> Sucesso: %d/%d\n", len(urls), len(images))
			} else {
				for _, u := range urls {
					fmt.Println(u)
				}
			}

			chapterMetadata := models.Chapter{
				Title:       ch,
				LastUpdated: fmt.Sprintf("%d", time.Now().Unix()),
				Groups: map[string][]string{
					groupName: urls,
				},
			}

			newData.Chapters[ch] = chapterMetadata
			existingJson = utils.MergeMetadata(existingJson, newData, "smart")

			if err := utils.SaveJSON(jsonPath, existingJson); err != nil {
				fmt.Fprintf(os.Stderr, "   [!] Aviso: Erro ao salvar checkpoint incremental: %v\n", err)
			} else {
				state.CompletedChapters[ch] = true
				_ = utils.SaveState(dbRoot, state)
			}
			newData.Chapters = make(map[string]models.Chapter)
		} else {
			fmt.Fprintf(os.Stderr, "   [X] Falha geral nas imagens do capítulo.\n")
		}
	}

	return p.uploadToGitHub(ctx, jsonPath, jsonFilename, effectiveID, ghFolder, useRoot, mangaTitle, quiet)
}

func (p *Pipeline) uploadToGitHub(ctx context.Context, jsonPath, jsonFilename, effectiveID, ghFolder string, useRoot bool, mangaTitle string, quiet bool) error {
	if !quiet {
		fmt.Println("\n>> Enviando metadados consolidados para o GitHub...")
	}
	if p.active.GitHubToken == "" {
		if !quiet {
			fmt.Println(">> [Aviso] github_token ausente. Pulando upload final no repositório.")
		}
		return nil
	}

	finalBytes, err := os.ReadFile(jsonPath)
	if err != nil {
		return fmt.Errorf("erro lendo JSON do disco: %v", err)
	}

	var remotePath string
	if useRoot || ghFolder == "" {
		remotePath = jsonFilename
	} else {
		remotePath = filepath.Join(ghFolder, jsonFilename)
	}

	remotePath = strings.ReplaceAll(remotePath, "\\", "/")

	err = p.client.UploadJSON(ctx, remotePath, finalBytes, fmt.Sprintf("Sincronização de mangá via CLI: %s", mangaTitle))
	if err != nil {
		return fmt.Errorf("falha ao submeter para o GitHub: %v", err)
	}

	if !quiet {
		fmt.Println(">> Metadados sincronizados com Sucesso!")
	}
	return nil
}

func (p *Pipeline) findImages(dir string) ([]string, error) {
	var images []string
	exts := map[string]bool{
		".jpg": true, ".jpeg": true, ".png": true, ".webp": true, ".gif": true,
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	for _, e := range entries {
		if !e.IsDir() {
			ext := strings.ToLower(filepath.Ext(e.Name()))
			if exts[ext] {
				images = append(images, filepath.Join(dir, e.Name()))
			}
		}
	}
	return images, nil
}

func (p *Pipeline) findCoverImage(mangaPath string) string {
	coverNames := []string{"cover", "folder", "thumb", "thumbnail", "001", "01", "1"}
	exts := []string{".jpg", ".jpeg", ".png", ".webp"}

	// 1. Look in root directory for common names
	for _, name := range coverNames {
		for _, ext := range exts {
			candidate := filepath.Join(mangaPath, name+ext)
			if _, err := os.Stat(candidate); err == nil {
				return candidate
			}
		}
	}

	// 2. If no explicit cover found, grab the first image of the first chapter
	entries, err := os.ReadDir(mangaPath)
	if err != nil {
		return ""
	}

	var chapters []string
	for _, e := range entries {
		if e.IsDir() {
			chapters = append(chapters, e.Name())
		}
	}

	if len(chapters) > 0 {
		utils.NaturalSort(chapters)
		firstChapterDir := filepath.Join(mangaPath, chapters[0])
		images, err := p.findImages(firstChapterDir)
		if err == nil && len(images) > 0 {
			utils.NaturalSort(images)
			return images[0]
		}
	}

	return ""
}
