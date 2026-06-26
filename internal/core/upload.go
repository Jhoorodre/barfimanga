package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
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

// sakuraChapter é o mínimo necessário do JSON do sakuramangas-dl.
type sakuraChapter struct {
	Volume int     `json:"volume"`
	Number float64 `json:"number"`
}

// loadSakuraVolumes lê um JSON do sakuramangas-dl e retorna mapa número→volume.
func loadSakuraVolumes(path string) map[float64]string {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var db struct {
		Chapters map[string]sakuraChapter `json:"chapters"`
	}
	if err := json.Unmarshal(data, &db); err != nil {
		return nil
	}
	m := make(map[float64]string, len(db.Chapters))
	for _, ch := range db.Chapters {
		if ch.Volume > 0 {
			m[ch.Number] = strconv.Itoa(ch.Volume)
		}
	}
	return m
}

// chapterKey formata a chave do capítulo a partir do nome da pasta.
// "Cap 019.1 - Título" → "019.1", "Cap 037 - Título" → "037", "Cap 000" → "000"
func chapterKey(folderName string) string {
	parts := strings.Fields(folderName)
	if len(parts) < 2 {
		return folderName
	}
	raw := parts[1]
	dotIdx := strings.IndexByte(raw, '.')
	intPart, decPart := raw, ""
	if dotIdx >= 0 {
		intPart = raw[:dotIdx]
		decPart = raw[dotIdx:] // ".1", ".2", ".5"
	}
	n, err := strconv.Atoi(intPart)
	if err != nil {
		return raw
	}
	return fmt.Sprintf("%03d%s", n, decPart)
}

// chapterNumberFromName extrai o número como float64 para lookup no mapa de volumes.
func chapterNumberFromName(name string) (float64, bool) {
	parts := strings.Fields(name)
	if len(parts) < 2 {
		return 0, false
	}
	n, err := strconv.ParseFloat(parts[1], 64)
	return n, err == nil
}

// formatChapterTitle monta o título canônico do capítulo.
// Com volume: "Vol.02 Ch.006 - Compatível"
// Sem volume (showVol=false ou volume=""): "Ch.006 - Compatível"
// Sem subtítulo: "Vol.02 Ch.006"
func formatChapterTitle(folderName, volume string, showVol bool) string {
	key := chapterKey(folderName)

	subtitle := ""
	if idx := strings.Index(folderName, " - "); idx >= 0 {
		subtitle = strings.TrimSpace(folderName[idx+3:])
	}

	title := "Ch." + key
	if showVol && volume != "" {
		if n, err := strconv.Atoi(volume); err == nil {
			title = fmt.Sprintf("Vol.%02d Ch.%s", n, key)
		}
	}
	if subtitle != "" {
		title += " - " + subtitle
	}
	return title
}

// logPipeline grava eventos no arquivo bd/pipeline.log
func logPipeline(msg string) {
	dir, err := os.Getwd()
	if err != nil {
		return
	}
	logPath := filepath.Join(dir, "bd", "pipeline.log")
	os.MkdirAll(filepath.Dir(logPath), 0755)

	f, err := os.OpenFile(logPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		defer f.Close()
		f.WriteString(time.Now().Format("2006-01-02 15:04:05") + " - " + msg + "\n")
	}
}

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
	if entry.ScanGroup != "" {
		groupName = entry.ScanGroup
	}

	mangaTitle := filepath.Base(dir)
	if entry.Name != "" {
		mangaTitle = entry.Name
	}
	mangaRoot := dir

	logPipeline(fmt.Sprintf("INICIANDO PIPELINE: Obra '%s' | Dir: %s | SyncOnly: %v", mangaTitle, dir, syncOnly))

	var sakuraVolumes map[float64]string
	if entry.SakuraMangasDB != "" {
		sakuraDBPath := utils.ToWSLPath(entry.SakuraMangasDB)
		if info, err := os.Stat(sakuraDBPath); err == nil && info.IsDir() {
			filename := strings.ToLower(strings.ReplaceAll(mangaTitle, " ", "_")) + ".json"
			sakuraDBPath = filepath.Join(sakuraDBPath, filename)
		}
		sakuraVolumes = loadSakuraVolumes(sakuraDBPath)
	}

	// showVol=true apenas quando há dados de volume E nem todos são "1"
	// (vol.1 universal = volume desconhecido/não mapeado na fonte)
	showVol := false
	for _, v := range sakuraVolumes {
		if v != "1" {
			showVol = true
			break
		}
	}

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
				if entry.Name == "" {
					mangaTitle = filepath.Base(mangaRoot)
				}
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
		folderName := filepath.Base(dir)
		safeDirName := utils.SanitizeFilename(folderName, false)
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

	// Rebuild: preserva metadados mas reconstrói capítulos do zero
	if forceRebuild && !syncOnly {
		existingJson.Chapters = make(map[string]models.Chapter)
	}

	// Se for APENAS Sync, atualiza metadados do cabeçalho e sobe pro GitHub
	if syncOnly {
		logPipeline(fmt.Sprintf("[Fast-Sync] Atualizando metadados de '%s'", mangaTitle))
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

	pool := worker.NewPool(p.host, p.active.Workers, p.active.RateLimit, p.active.MaxRetries)

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

		// Rebuild reconstrói o JSON do zero mas ainda usa cache de imagens para evitar re-uploads
		results, err := pool.ProcessImages(ctx, images, tracker, uploadCache, false)
		progUI.Finish(err == nil)

		if ctx.Err() != nil {
			fmt.Println("\n\n[!] Processo cancelado pelo usuário (Ctrl+C).")
			fmt.Println("[i] O progresso de todos os capítulos já concluídos foi salvo localmente com segurança.")
			logPipeline("ABORTADO: Cancelamento do usuário detectado no capítulo " + ch)
			return nil
		}

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

			volume := ""
			if sakuraVolumes != nil {
				if num, ok := chapterNumberFromName(ch); ok {
					volume = sakuraVolumes[num]
				}
			}
			chapterMetadata := models.Chapter{
				Title:       formatChapterTitle(ch, volume, showVol),
				Volume:      volume,
				LastUpdated: fmt.Sprintf("%d", time.Now().Unix()),
				Groups: map[string][]string{
					groupName: urls,
				},
			}

			newData.Chapters[chapterKey(ch)] = chapterMetadata
			existingJson = utils.MergeMetadata(existingJson, newData, "smart")

			if err := utils.SaveJSON(jsonPath, existingJson); err != nil {
				fmt.Fprintf(os.Stderr, "   [!] Aviso: Erro ao salvar checkpoint incremental: %v\n", err)
				logPipeline(fmt.Sprintf("ERRO: checkpoint JSON do cap %s: %v", ch, err))
			} else {
				state.CompletedChapters[ch] = true
				_ = utils.SaveState(dbRoot, state)
				logPipeline(fmt.Sprintf("SUCESSO: Cap %s concluído (%d imagens)", ch, len(urls)))
			}
			newData.Chapters = make(map[string]models.Chapter)
		} else {
			fmt.Fprintf(os.Stderr, "   [X] Falha geral nas imagens do capítulo.\n")
			logPipeline(fmt.Sprintf("FALHA TOTAL: Cap %s não teve imagens salvas", ch))
		}
	}

	logPipeline("FINALIZANDO PIPELINE de Upload. Chamando GitHub Sync...")
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

	err = p.client.UploadJSON(ctx, remotePath, finalBytes, fmt.Sprintf("Update %s", jsonFilename))
	if err != nil {
		logPipeline(fmt.Sprintf("ERRO GitHub Sync: %v", err))
		return fmt.Errorf("falha ao submeter para o GitHub: %v", err)
	}

	if !quiet {
		fmt.Println(">> Metadados sincronizados com Sucesso!")
	}
	logPipeline("SUCESSO GitHub Sync")

	// --- GERADOR DE LINKS CUBARI ---
	rawURL := fmt.Sprintf("https://raw.githubusercontent.com/%s/%s/%s", p.active.GitHubRepo, p.active.GitHubBranch, remotePath)
	cubariPath := fmt.Sprintf("raw/%s/refs/heads/%s/%s", p.active.GitHubRepo, p.active.GitHubBranch, remotePath)
	encodedURL := base64.RawURLEncoding.EncodeToString([]byte(cubariPath))
	cubariURL := fmt.Sprintf("https://cubari.moe/read/gist/%s", encodedURL)

	linkInfo := fmt.Sprintf("\n==============================\n"+
		"Nome: %s\nID do JSON: %s\n"+
		"Link Raw GitHub: %s\n"+
		"Link Cubari: %s\n"+
		"==============================\n",
		mangaTitle, effectiveID, rawURL, cubariURL)

	if !quiet {
		fmt.Println(linkInfo)
	}

	linksFile := filepath.Join("bd", "cubari_links.txt")
	f, err := os.OpenFile(linksFile, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		f.WriteString(linkInfo)
		f.Close()
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
