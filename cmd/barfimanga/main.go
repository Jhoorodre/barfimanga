// Package main é o ponto de entrada principal do BarfiManga CLI.
// Ele é responsável por gerenciar os argumentos de linha de comando (flags),
// carregar as configurações do usuário, resolver a precedência (CLI > Env > Config)
// e iniciar a aplicação, seja no modo interativo (TUI) ou no modo Headless (via comandos).
package main

import (
	"context"   // Usado para gerenciar timeouts e cancelamento seguro (Ctrl+C)
	"flag"      // Padrão do Go para ler parâmetros passados no terminal (ex: --force)
	"fmt"       // Formatação de entrada e saída (prints no console)
	"os"        // Acesso a variáveis de ambiente e encerramento do processo (os.Exit)
	"os/signal" // Intercepta sinais do sistema operacional (ex: SIGINT)
	"strconv"   // Converte strings em tipos primitivos numéricos
	"syscall"   // Definições de baixo nível do sistema (ex: constantes de sinais)

	"barfimanga/internal/config" // Gerencia o arquivo config.json e credenciais
	"barfimanga/internal/core"   // O coração da máquina: orquestra a leitura de pastas e uploads
	"barfimanga/internal/tui"    // Terminal User Interface: a interface gráfica no terminal
)

// cliOptions mantém em memória todas as bandeiras (flags) passadas pelo terminal na hora de rodar o programa.
// Ex: `barfimanga --force --sync-only` preenche esta estrutura.
type cliOptions struct {
	ConfigMode   string            // Exibe as configurações do usuário no terminal (ex: "show")
	Interactive  bool              // Ativa a interface visual do TUI (Menu bonitinho)
	Directory    string            // O caminho físico das pastas de imagens do mangá a ser upado
	Workers      int               // Quantas imagens subir paralelamente (se 0, usa a configuração padrão)
	Host         string            // Onde hospedar a imagem (ex: "imgbox", "catbox", "imgur")
	Quiet        bool              // Se verdadeiro, esconde as barras de progresso (útil para automação via bash)
	Recursive    bool              // Se verdadeiro, tentará procurar várias obras em sub-pastas (ainda não totalmente implementado)
	Retry        int               // Quantas vezes tentar upar a mesma imagem se o host cair
	Token        string            // API Key do serviço de hospedagem escolhido
	RateLimit    float64           // Limite de requisições por segundo para evitar que o host dê ban por excesso de tráfego
	Group        string            // A Scanlator associada ao upload (Aparece no JSON do site)
	MangaID      string            // ID único que vai virar o nome do arquivo json final
	GitHubFolder string            // Se o repo tiver mangás em pastas diferentes, especifica o local
	UseRoot      bool              // Ignora subpastas e joga o arquivo index direto no repositório root
	ForceRebuild bool              // [PERIGO] Se verdadeiro, ele deleta caches locais e sobe TODAS as imagens de novo, ignorando as que já deram sucesso
	SyncOnly     bool              // Se verdadeiro, ele não mexe com imagens, APENAS empurra o arquivo JSON localizado atualizado pro Github
	MangaEntry   config.MangaEntry // Objeto com todos os metadados (título, autor, descrição) capturado do TUI
}

// parseFlags captura todas as opções digitadas no terminal e mapeia na struct cliOptions
func parseFlags(args []string) (*cliOptions, error) {
	opts := &cliOptions{}
	fs := flag.NewFlagSet("barfimanga", flag.ContinueOnError)

	// Custom Usage
	fs.Usage = func() {
		fmt.Fprintf(os.Stderr, "Uso: %s [opções] [diretório]\n\nOpções:\n", fs.Name())
		fs.PrintDefaults()
	}

	fs.StringVar(&opts.ConfigMode, "config", "", "Gerenciar configurações (opções: show)")

	fs.BoolVar(&opts.Interactive, "interactive", false, "Iniciar modo interativo")
	fs.BoolVar(&opts.Interactive, "i", false, "Iniciar modo interativo (alias)")

	fs.StringVar(&opts.Directory, "dir", "", "Diretório contendo as imagens do mangá")
	fs.StringVar(&opts.Directory, "d", "", "Diretório contendo as imagens do mangá (alias)")

	fs.StringVar(&opts.Group, "group", "Default", "Nome do grupo de scan para os capítulos")
	fs.StringVar(&opts.Group, "g", "Default", "Nome do grupo de scan (alias)")

	fs.StringVar(&opts.MangaID, "id", "", "ID da obra (usado como nome do arquivo JSON)")

	fs.StringVar(&opts.GitHubFolder, "ghpath", "", "Pasta no GitHub (deixe vazio para raiz)")

	fs.BoolVar(&opts.UseRoot, "root", false, "Salvar o JSON na raiz (atalho para --ghpath '')")

	fs.BoolVar(&opts.ForceRebuild, "force", false, "Força o re-upload, ignorando cache e checkpoints")

	fs.BoolVar(&opts.SyncOnly, "sync-only", false, "Atualiza apenas os metadados (JSON) no GitHub")

	fs.IntVar(&opts.Workers, "workers", 0, "Número de workers paralelos (sobrescreve config)")
	fs.IntVar(&opts.Workers, "w", 0, "Número de workers paralelos (alias)")

	fs.StringVar(&opts.Host, "host", "", "Host de imagem padrão (ex: catbox, imgur)")
	fs.StringVar(&opts.Host, "h", "", "Host de imagem padrão (alias)")

	fs.BoolVar(&opts.Quiet, "quiet", false, "Modo silencioso (desativa a barra de progresso)")
	fs.BoolVar(&opts.Quiet, "q", false, "Modo silencioso (alias)")

	fs.BoolVar(&opts.Recursive, "recursive", false, "Upload recursivo de diretórios (futuro)")
	fs.BoolVar(&opts.Recursive, "r", false, "Upload recursivo de diretórios (alias)")

	fs.IntVar(&opts.Retry, "retry", 3, "Número de tentativas em caso de falha")

	fs.StringVar(&opts.Token, "token", "", "Token do host de imagem")
	fs.StringVar(&opts.Token, "t", "", "Token do host de imagem (alias)")

	fs.Float64Var(&opts.RateLimit, "ratelimit", 0, "Limite de requisições por segundo")

	err := fs.Parse(args)
	if err != nil {
		return nil, err
	}

	// Positional argument for directory if not provided via flag
	if opts.Directory == "" && fs.NArg() > 0 {
		opts.Directory = fs.Arg(0)
	}

	return opts, nil
}

// applyPrecedence mescla as configurações.
// O sistema respeita uma hierarquia de quem manda mais:
// 1º - O que foi passado na linha de comando (CLI Flags)
// 2º - Variáveis de Ambiente do Sistema Operacional (Env Vars)
// 3º - O arquivo base de configurações do usuário (~/.config/barfimanga/config.json)
func applyPrecedence(opts *cliOptions, cfg *config.Config) {
	// Nível 2: Se tem Variáveis de Ambiente (ex: .env ou export), elas sobrepõem o config.json
	if envWorkers := os.Getenv("MU_WORKERS"); envWorkers != "" {
		if w, err := strconv.Atoi(envWorkers); err == nil {
			cfg.Workers = w
		}
	}
	if envHost := os.Getenv("MU_HOST"); envHost != "" {
		cfg.DefaultHost = envHost
	}
	if envToken := os.Getenv("MU_TOKEN"); envToken != "" {
		cfg.HostToken = envToken
	}

	// Nível 1: Se o usuário passou diretamente pelo terminal (ex: --workers 10), esmaga tudo e obedece
	if opts.Workers > 0 {
		cfg.Workers = opts.Workers
	}
	if opts.Host != "" {
		cfg.DefaultHost = opts.Host
	}
	if opts.Token != "" {
		cfg.HostToken = opts.Token
	}
	if opts.RateLimit > 0 {
		cfg.RateLimit = opts.RateLimit
	}
}

// main é o maestro do CLI. Ele inicializa e dá o pontapé na execução correta baseada nas intenções do usuário.
func main() {
	// 1. Lê o que o usuário digitou no terminal
	opts, err := parseFlags(os.Args[1:])
	if err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Erro ao parsear flags: %v\n", err)
		os.Exit(2)
	}

	// Se ele só digitou 'barfimanga --config show', imprime as credenciais e fecha o programa.
	if opts.ConfigMode == "show" {
		mCfg, err := config.LoadConfig()
		if err != nil {
			fmt.Fprintf(os.Stderr, "Erro ao carregar configuração: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Perfil Ativo: %s\n", mCfg.ActiveProfile)
		active := mCfg.GetActive()
		fmt.Printf("Default Host: %s\n", active.DefaultHost)
		fmt.Printf("GitHub Repo: %s\n", active.GitHubRepo)
		fmt.Printf("Workers: %d\n", active.Workers)
		return
	}

	// 2. Carrega as configurações do disco (JSON salvo em ~/.config/barfimanga/config.json)
	mCfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao carregar configuração: %v\n", err)
		os.Exit(1)
	}

	// 3. Mescla o que o usuário digitou agora (CLI) em cima da configuração do disco
	active := mCfg.GetActive()
	applyPrecedence(opts, &active)
	mCfg.Profiles[mCfg.ActiveProfile] = active // Salva de volta no struct em memória

	// 4. Se o usuário só digitou "barfimanga" (sem pasta nem nada), força a abertura da TUI (interface gráfica de terminal)
	if opts.Directory == "" && opts.ConfigMode == "" {
		opts.Interactive = true
	}

	// O For { } mantém a interface viva. Se o usuário terminar de upar um mangá pela TUI, volta pro menu.
	for {
		var tasks []tui.UploadTask

		if opts.Interactive {
			var err error
			tasks, err = tui.RunInteractive(&mCfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Erro no modo interativo: %v\n", err)
				os.Exit(1)
			}
			if len(tasks) == 0 {
				return // Saiu de forma graciosa pressionando 'q' ou escolhendo Sair no menu
			}
		} else {
			if opts.Directory == "" {
				fmt.Println("Use -h ou --help para ver as opções.")
				return
			}
			tasks = []tui.UploadTask{{
				Directory:    opts.Directory,
				MangaID:      opts.MangaID,
				GitHubFolder: opts.GitHubFolder,
				ForceRebuild: opts.ForceRebuild,
				SyncOnly:     opts.SyncOnly,
				MangaEntry:   opts.MangaEntry,
			}}
		}

		// 5. Início do Motor Principal (Upload Pipeline)
		for _, task := range tasks {
			opts.Directory = task.Directory
			opts.MangaID = task.MangaID
			opts.GitHubFolder = task.GitHubFolder
			opts.ForceRebuild = task.ForceRebuild
			opts.SyncOnly = task.SyncOnly
			opts.MangaEntry = task.MangaEntry
			if task.GitHubFolder == "" {
				opts.UseRoot = true
			} else {
				opts.UseRoot = false
			}

			active := mCfg.GetActive()
			applyPrecedence(opts, &active)

			groupName := opts.Group
			if opts.Group == "Default" && active.ScanGroup != "" {
				groupName = active.ScanGroup
			}

			fmt.Printf("\n==============================================\n")
			fmt.Printf("Iniciando processo para: %s\n", opts.Directory)

			// Prepara escutador de "CTRL+C" por tarefa
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

			err := func() error {
				pipeline, err := core.NewPipeline(mCfg)
				if err != nil {
					return fmt.Errorf("erro ao configurar pipeline: %v", err)
				}
				return pipeline.Run(ctx, opts.Directory, opts.Quiet, groupName, opts.MangaID, opts.GitHubFolder, opts.UseRoot, opts.ForceRebuild, opts.MangaEntry, opts.SyncOnly)
			}()

			stop() // Libera contexto

			if err != nil {
				fmt.Fprintf(os.Stderr, "Erro crítico na execução: %v\n", err)
			}

			// Se o usuário cancelou via Ctrl+C, interrompe todo o lote
			if ctx.Err() != nil {
				fmt.Println("\n[!] Cancelamento global detectado. Interrompendo fila de lote...")
				break
			}
		}

		if opts.Interactive {
			fmt.Println("\n[!] Trabalho concluído. Retornando ao menu...")
			opts.Directory = "" // Limpa a pasta
			continue
		}

		return // Headless normal, finaliza o processo
	}
}
