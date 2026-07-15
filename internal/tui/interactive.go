package tui

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"barfimanga/internal/config"
	"barfimanga/internal/utils"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/huh"
	"github.com/joho/godotenv"
)

// UploadTask representa uma obra a ser processada pelo motor
type UploadTask struct {
	Directory    string
	MangaID      string
	GitHubFolder string
	ForceRebuild bool
	SyncOnly     bool
	MangaEntry   config.MangaEntry
}

// RunInteractive inicia a interface de usuário no terminal (TUI).
func RunInteractive(mCfg *config.MultiConfig) ([]UploadTask, error) {
	for {
		var action string

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(fmt.Sprintf("BarfiManga - Menu Principal (Perfil: %s)", mCfg.ActiveProfile)).
					Options(
						huh.NewOption("Fazer Upload de Obra Salva", "library_upload"),
						huh.NewOption("Upload em Lote (Múltiplas Obras)", "batch_upload"),
						huh.NewOption("Upload Rápido (Sem salvar)", "quick_upload"),
						huh.NewOption("Gerenciar Biblioteca", "library_manage"),
						huh.NewOption("Gerenciar Perfis", "profiles_manage"),
						huh.NewOption("Sair", "exit"),
					).
					Value(&action),
			),
		)

		if err := form.Run(); err != nil {
			return nil, err
		}

		switch action {
		case "library_upload":
			task, err := selectFromLibrary(mCfg)
			if err != nil || task.Directory == "" {
				continue
			}
			return []UploadTask{task}, nil
		case "batch_upload":
			tasks, err := selectBatchFromLibrary(mCfg)
			if err != nil || len(tasks) == 0 {
				continue
			}
			return tasks, nil

		case "quick_upload":
			var dirPath string
			var mangaID string
			var githubFolder string
			var forceRebuild bool

			dirForm := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Caminho do diretório do mangá").
						Description("Ex: /home/user/Downloads/Mangas/Naruto").
						Value(&dirPath).
						Validate(func(str string) error {
							if str == "" {
								return fmt.Errorf("o caminho não pode ser vazio")
							}
							translated := utils.ToWSLPath(str)
							info, err := os.Stat(translated)
							if os.IsNotExist(err) {
								return fmt.Errorf("diretório não existe")
							}
							if !info.IsDir() {
								return fmt.Errorf("o caminho deve ser um diretório")
							}
							return nil
						}),
				),
				huh.NewGroup(
					huh.NewInput().
						Title("ID da Obra").
						Description("Será usado como nome do arquivo JSON. Vazio = Nome da pasta.").
						Value(&mangaID),
					huh.NewInput().
						Title("Pasta no GitHub").
						Description("Ex: obras/mangas. Deixe VAZIO para salvar na raiz.").
						Value(&githubFolder),
					huh.NewConfirm().
						Title("Forçar Re-upload (Rebuild)?").
						Description("Ignora cache e arquivos de estado para enviar tudo novamente.").
						Value(&forceRebuild),
				),
			)

			if err := dirForm.Run(); err != nil {
				continue
			}
			return []UploadTask{{
				Directory:    utils.ToWSLPath(dirPath),
				MangaID:      mangaID,
				GitHubFolder: githubFolder,
				ForceRebuild: forceRebuild,
				SyncOnly:     false,
				MangaEntry:   config.MangaEntry{},
			}}, nil

		case "library_manage":
			_ = manageLibrary(mCfg)

		case "profiles_manage":
			if err := manageProfiles(mCfg); err != nil {
				continue
			}

		case "exit":
			os.Exit(0)
		}
	}
}

func selectFromLibrary(mCfg *config.MultiConfig) (UploadTask, error) {
	prof := mCfg.Profiles[mCfg.ActiveProfile]
	if len(prof.Library) == 0 {
		fmt.Println("\n[!] Sua biblioteca está vazia neste perfil.")
		return UploadTask{}, nil
	}

	var options []huh.Option[string]
	for _, entry := range prof.Library {
		options = append(options, huh.NewOption(entry.Name, entry.Name))
	}
	options = append(options, huh.NewOption("<- Voltar", "back"))

	var selectedName string
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Selecionar Obra").
				Options(options...).
				Value(&selectedName),
		),
	)

	if err := form.Run(); err != nil || selectedName == "back" {
		return UploadTask{}, nil
	}

	entry := prof.Library[selectedName]

	// Sub-menu de ações para a obra selecionada
	var action string
	actionForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Ações para: "+entry.Name).
				Options(
					huh.NewOption("Fazer Upload Completo (Imagens + JSON)", "upload"),
					huh.NewOption("Sincronizar Apenas Metadados (JSON)", "sync"),
					huh.NewOption("<- Voltar", "back"),
				).
				Value(&action),
		),
	)

	if err := actionForm.Run(); err != nil || action == "back" {
		return UploadTask{}, nil
	}

	if action == "sync" {
		return UploadTask{
			Directory:    utils.ToWSLPath(entry.LocalPath),
			MangaID:      entry.MangaID,
			GitHubFolder: entry.GitHubFolder,
			ForceRebuild: false,
			SyncOnly:     true,
			MangaEntry:   entry,
		}, nil
	}

	var forceRebuild bool
	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Iniciar Upload de '%s'?", entry.Name)).
				Description("Caminho: "+entry.LocalPath).
				Value(new(bool)), // Apenas visual
			huh.NewConfirm().
				Title("Forçar Re-upload (Rebuild)?").
				Value(&forceRebuild),
		),
	)

	if err := confirmForm.Run(); err != nil {
		return UploadTask{}, nil
	}

	return UploadTask{
		Directory:    utils.ToWSLPath(entry.LocalPath),
		MangaID:      entry.MangaID,
		GitHubFolder: entry.GitHubFolder,
		ForceRebuild: forceRebuild,
		SyncOnly:     false,
		MangaEntry:   entry,
	}, nil
}

func selectBatchFromLibrary(mCfg *config.MultiConfig) ([]UploadTask, error) {
	prof := mCfg.Profiles[mCfg.ActiveProfile]
	if len(prof.Library) == 0 {
		fmt.Println("\n[!] Sua biblioteca está vazia neste perfil.")
		return nil, nil
	}

	var mode string
	modeForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Modo de Seleção do Lote").
				Options(
					huh.NewOption("Seleção Manual Visual (Checklist)", "manual"),
					huh.NewOption("Seleção Dicionário Numerado (Ex: 1, 15, 20)", "numbered"),
					huh.NewOption("Colar Lista (IDs ou Nomes)", "paste"),
					huh.NewOption("Inverter Seleção (Todas MENOS as escolhidas)", "inverse"),
					huh.NewOption("Selecionar TODAS as obras", "all"),
					huh.NewOption("Filtro: Atualizados Recentemente (ModTime)", "recent"),
					huh.NewOption("Filtro: Obras 'Virgens' (Nunca Enviadas)", "virgin"),
					huh.NewOption("Filtro: Metadados Incompletos (Auditoria)", "audit"),
					huh.NewOption("Filtro: Status", "status"),
					huh.NewOption("Filtro: Ordem Alfabética (Letra)", "alpha"),
					huh.NewOption("<- Voltar", "back"),
				).
				Value(&mode),
		),
	)

	if err := modeForm.Run(); err != nil || mode == "back" {
		return nil, nil
	}

	var selectedNames []string

	if mode == "manual" {
		var options []huh.Option[string]
		for _, entry := range prof.Library {
			options = append(options, huh.NewOption(entry.Name, entry.Name))
		}
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewMultiSelect[string]().Title("Selecionar Obras (Espaço = marcar, Esc = cancelar)").Options(options...).Value(&selectedNames),
			),
		)
		if err := form.Run(); err != nil {
			return nil, nil
		}

	} else if mode == "numbered" {
		var sortedNames []string
		for name := range prof.Library {
			sortedNames = append(sortedNames, name)
		}
		sort.Strings(sortedNames)

		fmt.Println("\n--- DICIONÁRIO DE OBRAS ---")
		for i, name := range sortedNames {
			fmt.Printf("[%3d] %s\n", i+1, name)
		}
		fmt.Println("---------------------------")

		fmt.Print("Digite os números (ex: 1, 5). Aperte ENTER vazio para VOLTAR: ")
		scanner := bufio.NewScanner(os.Stdin)
		scanner.Scan()
		inputStr := scanner.Text()

		if strings.TrimSpace(inputStr) == "" {
			return nil, nil
		}

		parts := strings.Split(inputStr, ",")
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p == "" {
				continue
			}
			idx, err := strconv.Atoi(p)
			if err == nil && idx >= 1 && idx <= len(sortedNames) {
				selectedNames = append(selectedNames, sortedNames[idx-1])
			}
		}

	} else if mode == "paste" {
		var pasted string
		pasteForm := huh.NewForm(huh.NewGroup(huh.NewText().Title("Cole a lista de Nomes/IDs (Aperte ENTER vazio p/ VOLTAR)").Value(&pasted).Lines(8)))
		if err := pasteForm.Run(); err != nil || strings.TrimSpace(pasted) == "" {
			return nil, nil
		}

		pasted = strings.ReplaceAll(pasted, "\n", ",")
		parts := strings.Split(pasted, ",")
		lookup := make(map[string]bool)
		for _, p := range parts {
			p = strings.TrimSpace(p)
			if p != "" {
				lookup[strings.ToLower(p)] = true
			}
		}
		for _, entry := range prof.Library {
			if lookup[strings.ToLower(entry.Name)] || lookup[strings.ToLower(entry.MangaID)] {
				selectedNames = append(selectedNames, entry.Name)
			}
		}

	} else if mode == "inverse" {
		var options []huh.Option[string]
		for _, entry := range prof.Library {
			options = append(options, huh.NewOption(entry.Name, entry.Name))
			selectedNames = append(selectedNames, entry.Name) // Pré-seleciona todas!
		}
		form := huh.NewForm(huh.NewGroup(huh.NewMultiSelect[string]().Title("Desmarque as que NÃO quer (Esc = cancelar)").Options(options...).Value(&selectedNames)))
		if err := form.Run(); err != nil || len(selectedNames) == 0 {
			return nil, nil
		}

	} else if mode == "all" {
		for _, entry := range prof.Library {
			selectedNames = append(selectedNames, entry.Name)
		}

	} else if mode == "recent" {
		var daysStr string
		daysForm := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Atualizadas nos últimos X dias? (Aperte ENTER vazio p/ VOLTAR)").Value(&daysStr)))
		if err := daysForm.Run(); err != nil || strings.TrimSpace(daysStr) == "" {
			return nil, nil
		}

		days, err := strconv.Atoi(daysStr)
		if err != nil {
			days = 7
		}

		for _, entry := range prof.Library {
			info, err := os.Stat(utils.ToWSLPath(entry.LocalPath))
			if err == nil {
				if time.Since(info.ModTime()).Hours() <= float64(days*24) {
					selectedNames = append(selectedNames, entry.Name)
				}
			}
		}

	} else if mode == "virgin" {
		for _, entry := range prof.Library {
			dbRoot := utils.ToWSLPath(entry.LocalPath)
			if entry.MetadataPath != "" {
				dbRoot = filepath.Join(utils.ToWSLPath(entry.MetadataPath), utils.SanitizeFilename(filepath.Base(dbRoot), false))
			}
			statePath := filepath.Join(dbRoot, ".upload_state.json")
			if _, err := os.Stat(statePath); os.IsNotExist(err) {
				selectedNames = append(selectedNames, entry.Name)
			}
		}

	} else if mode == "audit" {
		for _, entry := range prof.Library {
			if entry.Author == "" || entry.Cover == "" || entry.MangaID == "" || entry.Description == "" {
				selectedNames = append(selectedNames, entry.Name)
			}
		}

	} else if mode == "status" {
		var selectedStatus string
		statusForm := huh.NewForm(huh.NewGroup(huh.NewSelect[string]().Title("Filtro Status").Options(huh.NewOption("Em Andamento", "Em Andamento"), huh.NewOption("Finalizado", "Finalizado"), huh.NewOption("Cancelado", "Cancelado"), huh.NewOption("Pausado", "Pausado"), huh.NewOption("<- Voltar", "back")).Value(&selectedStatus)))
		if err := statusForm.Run(); err != nil || selectedStatus == "back" {
			return nil, nil
		}
		for _, entry := range prof.Library {
			if entry.Status == selectedStatus {
				selectedNames = append(selectedNames, entry.Name)
			}
		}

	} else if mode == "alpha" {
		var letters string
		alphaForm := huh.NewForm(huh.NewGroup(huh.NewInput().Title("Letras iniciais (Ex: A, B. Aperte ENTER vazio p/ VOLTAR)").Value(&letters)))
		if err := alphaForm.Run(); err != nil || strings.TrimSpace(letters) == "" {
			return nil, nil
		}

		parts := strings.Split(strings.ToUpper(letters), ",")
		for _, entry := range prof.Library {
			upperName := strings.ToUpper(entry.Name)
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" && strings.HasPrefix(upperName, p) {
					selectedNames = append(selectedNames, entry.Name)
					break
				}
			}
		}
	}

	if len(selectedNames) == 0 {
		fmt.Println("\n[!] Nenhum mangá atendeu aos critérios do filtro ou operação cancelada.")
		return nil, nil
	}

	fmt.Printf("\n[+] %d obras selecionadas para o lote:\n", len(selectedNames))
	for i, name := range selectedNames {
		fmt.Printf("   %d. %s\n", i+1, name)
	}
	fmt.Println()

	fmt.Print("Pressione ENTER para continuar, ou digite 'v' para VOLTAR: ")
	scanner := bufio.NewScanner(os.Stdin)
	scanner.Scan()
	if strings.ToLower(strings.TrimSpace(scanner.Text())) == "v" {
		return nil, nil
	}

	var action string
	actionForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title(fmt.Sprintf("Ações para %d obras selecionadas", len(selectedNames))).
				Options(
					huh.NewOption("Fazer Upload Completo (Imagens + JSON)", "upload"),
					huh.NewOption("Sincronizar Apenas Metadados (JSON)", "sync"),
					huh.NewOption("<- Voltar", "back"),
				).
				Value(&action),
		),
	)

	if err := actionForm.Run(); err != nil || action == "back" {
		return nil, nil
	}

	var forceRebuild bool
	if action == "upload" {
		confirmForm := huh.NewForm(
			huh.NewGroup(
				huh.NewConfirm().
					Title("Forçar Re-upload (Rebuild) em todas?").
					Value(&forceRebuild),
			),
		)
		if err := confirmForm.Run(); err != nil {
			return nil, nil
		}
	}

	var tasks []UploadTask
	for _, name := range selectedNames {
		entry := prof.Library[name]
		tasks = append(tasks, UploadTask{
			Directory:    utils.ToWSLPath(entry.LocalPath),
			MangaID:      entry.MangaID,
			GitHubFolder: entry.GitHubFolder,
			ForceRebuild: forceRebuild,
			SyncOnly:     action == "sync",
			MangaEntry:   entry,
		})
	}

	return tasks, nil
}

func manageLibrary(mCfg *config.MultiConfig) error {
	for {
		prof := mCfg.Profiles[mCfg.ActiveProfile]
		var options []huh.Option[string]
		for _, entry := range prof.Library {
			options = append(options, huh.NewOption(entry.Name, entry.Name))
		}
		options = append(options, huh.NewOption("+ Adicionar Nova Obra", "add"))
		options = append(options, huh.NewOption("<- Voltar", "back"))

		var selectedName string
		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Gerenciar Biblioteca (Perfil: " + mCfg.ActiveProfile + ")").
					Options(options...).
					Value(&selectedName),
			),
		)

		if err := form.Run(); err != nil || selectedName == "back" {
			return nil
		}

		if selectedName == "add" {
			entry := config.MangaEntry{}
			if err := editMangaEntry(&entry); err == nil {
				prof.Library[entry.Name] = entry
				mCfg.Profiles[mCfg.ActiveProfile] = prof
				_ = config.SaveConfig(*mCfg)
			}
		} else {
			// Sub-menu de gerenciamento da obra específica
			entry := prof.Library[selectedName]
			var manageAction string
			manageForm := huh.NewForm(
				huh.NewGroup(
					huh.NewSelect[string]().
						Title("Gerenciar: "+entry.Name).
						Options(
							huh.NewOption("Editar Informações", "edit"),
							huh.NewOption("Excluir da Biblioteca", "delete"),
							huh.NewOption("<- Voltar", "back"),
						).
						Value(&manageAction),
				),
			)

			if err := manageForm.Run(); err != nil || manageAction == "back" {
				continue
			}

			if manageAction == "edit" {
				oldName := entry.Name
				oldMeta := entry.MetadataPath
				oldLocal := entry.LocalPath
				oldID := entry.MangaID

				if err := editMangaEntry(&entry); err == nil {
					// Lógica de movimentação automática de metadados
					if entry.AutoMoveMetadata && (oldMeta != entry.MetadataPath || oldLocal != entry.LocalPath) {
						oldRoot := utils.ToWSLPath(oldLocal)
						oldFolderBase := filepath.Base(oldRoot)
						if oldMeta != "" {
							oldRoot = filepath.Join(utils.ToWSLPath(oldMeta), utils.SanitizeFilename(oldFolderBase, false))
						}

						newRoot := utils.ToWSLPath(entry.LocalPath)
						newFolderBase := filepath.Base(newRoot)
						if entry.MetadataPath != "" {
							newRoot = filepath.Join(utils.ToWSLPath(entry.MetadataPath), utils.SanitizeFilename(newFolderBase, false))
						}

						if oldRoot != newRoot {
							os.MkdirAll(newRoot, 0755)

							oldEffectiveID := oldID
							if oldEffectiveID == "" {
								oldEffectiveID = utils.SanitizeFilename(oldName, false)
							}
							newEffectiveID := entry.MangaID
							if newEffectiveID == "" {
								newEffectiveID = utils.SanitizeFilename(entry.Name, false)
							}

							filesToMove := map[string]string{
								".manga_cache.json":      ".manga_cache.json",
								".upload_state.json":     ".upload_state.json",
								oldEffectiveID + ".json": newEffectiveID + ".json",
							}

							for oldFile, newFile := range filesToMove {
								oldFilePath := filepath.Join(oldRoot, oldFile)
								newFilePath := filepath.Join(newRoot, newFile)
								if _, err := os.Stat(oldFilePath); err == nil {
									utils.MoveFile(oldFilePath, newFilePath)
								}
							}

							// Tenta remover a pasta antiga caso ela tenha ficado vazia
							os.Remove(oldRoot)
						}
					}

					if oldName != entry.Name {
						delete(prof.Library, oldName)
					}
					prof.Library[entry.Name] = entry
					mCfg.Profiles[mCfg.ActiveProfile] = prof
					_ = config.SaveConfig(*mCfg)
				}
			} else if manageAction == "delete" {
				var confirmDelete bool
				confForm := huh.NewForm(
					huh.NewGroup(
						huh.NewConfirm().
							Title("Tem certeza que deseja excluir '" + entry.Name + "'?").
							Description("Isso apenas remove da biblioteca do CLI, não apaga arquivos.").
							Value(&confirmDelete),
					),
				)
				if err := confForm.Run(); err == nil && confirmDelete {
					delete(prof.Library, entry.Name)
					mCfg.Profiles[mCfg.ActiveProfile] = prof
					_ = config.SaveConfig(*mCfg)
				}
			}
		}
	}
}

func editMangaEntry(entry *config.MangaEntry) error {
	// Descrição é multi-linha: tira "enter" do avanço de campo (deixa só "tab")
	// para colar textos com várias linhas não espalhar pelos campos seguintes.
	km := huh.NewDefaultKeyMap()
	km.Text.Next = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "next"))
	km.Text.Submit = key.NewBinding(key.WithKeys("tab"), key.WithHelp("tab", "submit"))
	km.Text.NewLine = key.NewBinding(key.WithKeys("enter", "alt+enter", "ctrl+j"), key.WithHelp("enter", "new line"))

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Nome da Obra (Exibição)").
				Description("O nome real do mangá que aparecerá no site. Ex: 'Tower Dungeon'").
				Value(&entry.Name),
			huh.NewInput().Title("Caminho Local (Imagens)").
				Description("Pasta no seu HD onde ficam os capítulos baixados. Ex: '/mnt/c/Mangas/TD'").
				Value(&entry.LocalPath),
			huh.NewInput().Title("Caminho do Banco de Dados (JSON/Cache) - Opcional").
				Description("Deixe vazio para usar a mesma pasta das imagens acima.").
				Value(&entry.MetadataPath),
			huh.NewConfirm().Title("Mover Metadados Automaticamente?").
				Description("Se você mudou o caminho acima, o sistema move os arquivos para a nova pasta.").
				Value(&entry.AutoMoveMetadata),
			huh.NewInput().Title("Manga ID (JSON)").
				Description("Como o arquivo se chamará no Github. Ex: 'tower_dungeon' gera tower_dungeon.json").
				Value(&entry.MangaID),
			huh.NewInput().Title("Pasta no GitHub").
				Description("Onde salvar o JSON no repositório. Ex: 'obras'. Deixe vazio para a pasta raiz.").
				Value(&entry.GitHubFolder),
		),
		huh.NewGroup(
			huh.NewText().Title("Descrição").
				Description("A sinopse do mangá. Enter pula linha, Tab avança. Ctrl+E abre editor externo (bom p/ colar texto grande).").
				Value(&entry.Description),
			huh.NewInput().Title("Autor").
				Description("Nome do autor da história. Ex: 'Nihei Tsutomu'").
				Value(&entry.Author),
			huh.NewInput().Title("Artista").
				Description("Nome do desenhista. Deixe em branco se for o mesmo.").
				Value(&entry.Artist),
			huh.NewInput().Title("URL da Capa").
				Description("Link direto para uma imagem JPG/PNG. Ex: 'https://site.com/capa.jpg'").
				Value(&entry.Cover),
			huh.NewInput().Title("Status").
				Description("Ex: 'Em Andamento', 'Finalizado', 'Hiato'").
				Value(&entry.Status),
			huh.NewInput().Title("Scan Group (Opcional)").
				Description("Grupo responsável. Ex: 'Eremita Scan'. Deixe vazio para usar o padrão do perfil.").
				Value(&entry.ScanGroup),
			huh.NewInput().Title("Metadados Sakura (Opcional)").
				Description("Caminho para o .json do sakuramangas-dl. Preenche volumes automaticamente por capítulo.").
				Value(&entry.SakuraMangasDB),
		),
	).WithKeyMap(km)

	return form.Run()
}

func manageProfiles(mCfg *config.MultiConfig) error {
	for {
		var profileAction string

		// Prepara opções dinamicamente baseadas nos perfis existentes
		var options []huh.Option[string]
		for name := range mCfg.Profiles {
			label := fmt.Sprintf("Editar '%s'", name)
			if name == mCfg.ActiveProfile {
				label += " (Ativo)"
			}
			options = append(options, huh.NewOption(label, "edit_"+name))
		}
		options = append(options, huh.NewOption("+ Criar Novo Perfil", "create"))
		options = append(options, huh.NewOption("<- Voltar ao Menu", "back"))

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title("Gerenciar Perfis").
					Description("Selecione um perfil para ativá-lo e editá-lo").
					Options(options...).
					Value(&profileAction),
			),
		)

		if err := form.Run(); err != nil {
			return err
		}

		if profileAction == "back" {
			return nil
		}

		if profileAction == "create" {
			var newName string
			inputForm := huh.NewForm(
				huh.NewGroup(
					huh.NewInput().
						Title("Nome do novo perfil").
						Value(&newName).
						Validate(func(str string) error {
							if str == "" {
								return fmt.Errorf("o nome não pode ser vazio")
							}
							if _, exists := mCfg.Profiles[str]; exists {
								return fmt.Errorf("este perfil já existe")
							}
							return nil
						}),
				),
			)
			if err := inputForm.Run(); err != nil {
				continue // Volta pro gerenciamento de perfis se o usuário der Esc
			}
			mCfg.Profiles[newName] = config.GetDefaultConfig()
			mCfg.ActiveProfile = newName
			_ = config.SaveConfig(*mCfg)
			_ = editProfile(mCfg, newName)
		} else {
			// É uma edição
			name := profileAction[5:] // Remove o prefixo "edit_"
			mCfg.ActiveProfile = name
			_ = config.SaveConfig(*mCfg)
			_ = editProfile(mCfg, name)
		}
	}
}

func editProfile(mCfg *config.MultiConfig, name string) error {
	cfg := mCfg.Profiles[name]
	workersStr := strconv.Itoa(cfg.Workers)
	rateLimitStr := fmt.Sprintf("%.2f", cfg.RateLimit)
	var newPat string

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("GitHub Repo (owner/repo)").
				Description("O dono e o nome do repositório. Ex: 'Jhoorodre/barfimanga'").
				Value(&cfg.GitHubRepo),
			huh.NewInput().Title("GitHub Branch").
				Description("A branch principal do seu repositório. Ex: 'main' ou 'master'").
				Value(&cfg.GitHubBranch),
			huh.NewInput().Title("Nome da Variável do Token (.env)").
				Description("Ex: PAT_SAKURA_MANGAS. O programa vai ler esse nome no arquivo .env").
				Value(&cfg.GitHubTokenEnv),
			huh.NewInput().Title("Atualizar Valor do Token no .env (Opcional)").
				Description("Cole aqui o PAT para salvar no .env. Deixe vazio para manter o atual.").
				Value(&newPat).EchoMode(huh.EchoModePassword),
		),
		huh.NewGroup(
			huh.NewInput().Title("Scan Group Name").Description("Ex: MyScan").Value(&cfg.ScanGroup),
			huh.NewSelect[string]().
				Title("Default Host (Provedor)").
				Options(
					huh.NewOption("Catbox", "catbox"),
					huh.NewOption("Imgur", "imgur"),
					huh.NewOption("ImgBB", "imgbb"),
					huh.NewOption("ImageChest", "imagechest"),
					huh.NewOption("ImgHippo", "imghippo"),
					huh.NewOption("Lensdump", "lensdump"),
					huh.NewOption("Pixeldrain", "pixeldrain"),
					huh.NewOption("ImgPile", "imgpile"),
					huh.NewOption("ImgBox", "imgbox"),
					huh.NewOption("Google Drive (lh3)", "gdrive"),
				).
				Value(&cfg.DefaultHost),
			huh.NewInput().Title("Host Token (API Key / Client-ID)").Value(&cfg.HostToken).EchoMode(huh.EchoModePassword),
		),
		huh.NewGroup(
			huh.NewInput().
				Title("Workers (Paralelismo)").
				Description("Recomendado: 5 para Catbox, 1-3 para Imgur/ImgBB").
				Value(&workersStr),
			huh.NewInput().
				Title("Rate Limit (Requisições por segundo)").
				Description("Recomendado: 1.0 a 5.0 dependendo do Host").
				Value(&rateLimitStr),
		),
	)

	err := form.Run()
	if err != nil {
		return err // Volta silenciosamente se o usuário cancelar
	}

	if w, err := strconv.Atoi(workersStr); err == nil && w > 0 {
		cfg.Workers = w
	}
	if r, err := strconv.ParseFloat(rateLimitStr, 64); err == nil && r > 0 {
		cfg.RateLimit = r
	}

	mCfg.Profiles[name] = cfg
	if err := config.SaveConfig(*mCfg); err != nil {
		fmt.Printf("Erro ao salvar a configuração no disco: %v\n", err)
	}

	if newPat != "" && cfg.GitHubTokenEnv != "" {
		envPath := ".env" // godotenv lê e escreve da raiz do CWD
		envMap, err := godotenv.Read(envPath)
		if err != nil {
			envMap = make(map[string]string)
		}
		envMap[cfg.GitHubTokenEnv] = newPat
		if err := godotenv.Write(envMap, envPath); err == nil {
			fmt.Printf("\n[+] Token atualizado e salvo com sucesso no arquivo %s!\n", envPath)
			time.Sleep(1500 * time.Millisecond)
		} else {
			fmt.Printf("\n[!] Erro ao salvar o arquivo %s: %v\n", envPath, err)
			time.Sleep(2 * time.Second)
		}
	}

	return nil
}
