package tui

import (
	"fmt"
	"os"
	"strconv"

	"github.com/charmbracelet/huh"
	"barfimanga/internal/config"
	"barfimanga/internal/utils"
)

// RunInteractive inicia a interface de usuário no terminal (TUI).
func RunInteractive(mCfg *config.MultiConfig) (string, string, string, bool, bool, config.MangaEntry, error) {
	for {
		var action string

		form := huh.NewForm(
			huh.NewGroup(
				huh.NewSelect[string]().
					Title(fmt.Sprintf("BarfiManga - Menu Principal (Perfil: %s)", mCfg.ActiveProfile)).
					Options(
						huh.NewOption("Fazer Upload de Obra Salva", "library_upload"),
						huh.NewOption("Upload Rápido (Sem salvar)", "quick_upload"),
						huh.NewOption("Gerenciar Biblioteca", "library_manage"),
						huh.NewOption("Gerenciar Perfis", "profiles_manage"),
						huh.NewOption("Sair", "exit"),
					).
					Value(&action),
			),
		)

		if err := form.Run(); err != nil {
			return "", "", "", false, false, config.MangaEntry{}, err
		}

		switch action {
		case "library_upload":
			dir, id, gh, rebuild, syncOnly, entry, err := selectFromLibrary(mCfg)
			if err != nil || dir == "" {
				continue
			}
			return dir, id, gh, rebuild, syncOnly, entry, nil

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
			return utils.ToWSLPath(dirPath), mangaID, githubFolder, forceRebuild, false, config.MangaEntry{}, nil

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

func selectFromLibrary(mCfg *config.MultiConfig) (string, string, string, bool, bool, config.MangaEntry, error) {
	prof := mCfg.Profiles[mCfg.ActiveProfile]
	if len(prof.Library) == 0 {
		fmt.Println("\n[!] Sua biblioteca está vazia neste perfil.")
		return "", "", "", false, false, config.MangaEntry{}, nil
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
		return "", "", "", false, false, config.MangaEntry{}, nil
	}

	entry := prof.Library[selectedName]
	
	// Sub-menu de ações para a obra selecionada
	var action string
	actionForm := huh.NewForm(
		huh.NewGroup(
			huh.NewSelect[string]().
				Title("Ações para: " + entry.Name).
				Options(
					huh.NewOption("Fazer Upload Completo (Imagens + JSON)", "upload"),
					huh.NewOption("Sincronizar Apenas Metadados (JSON)", "sync"),
					huh.NewOption("<- Voltar", "back"),
				).
				Value(&action),
		),
	)
	
	if err := actionForm.Run(); err != nil || action == "back" {
		return "", "", "", false, false, config.MangaEntry{}, nil
	}

	if action == "sync" {
		return utils.ToWSLPath(entry.LocalPath), entry.MangaID, entry.GitHubFolder, false, true, entry, nil
	}

	var forceRebuild bool
	confirmForm := huh.NewForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("Iniciar Upload de '%s'?", entry.Name)).
				Description("Caminho: " + entry.LocalPath).
				Value(new(bool)), // Apenas visual
			huh.NewConfirm().
				Title("Forçar Re-upload (Rebuild)?").
				Value(&forceRebuild),
		),
	)

	if err := confirmForm.Run(); err != nil {
		return "", "", "", false, false, config.MangaEntry{}, nil
	}

	return utils.ToWSLPath(entry.LocalPath), entry.MangaID, entry.GitHubFolder, forceRebuild, false, entry, nil
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
						Title("Gerenciar: " + entry.Name).
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
				if err := editMangaEntry(&entry); err == nil {
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
	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("Nome da Obra (Exibição)").Value(&entry.Name),
			huh.NewInput().Title("Caminho Local (Imagens)").Value(&entry.LocalPath),
			huh.NewInput().Title("Caminho do Banco de Dados (JSON/Cache) - Opcional").
				Description("Deixe vazio para usar a mesma pasta das imagens.").
				Value(&entry.MetadataPath),
			huh.NewInput().Title("Manga ID (JSON)").Value(&entry.MangaID),
			huh.NewInput().Title("Pasta no GitHub").Value(&entry.GitHubFolder),
		),
		huh.NewGroup(
			huh.NewText().Title("Descrição").Value(&entry.Description),
			huh.NewInput().Title("Autor").Value(&entry.Author),
			huh.NewInput().Title("Artista").Value(&entry.Artist),
			huh.NewInput().Title("URL da Capa").Value(&entry.Cover),
			huh.NewInput().Title("Status").Value(&entry.Status),
		),
	)

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

	form := huh.NewForm(
		huh.NewGroup(
			huh.NewInput().Title("GitHub Repo (owner/repo)").Value(&cfg.GitHubRepo),
			huh.NewInput().Title("GitHub Branch").Value(&cfg.GitHubBranch),
			huh.NewInput().Title("GitHub Token (PAT)").Value(&cfg.GitHubToken).EchoMode(huh.EchoModePassword),
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

	return nil
}
