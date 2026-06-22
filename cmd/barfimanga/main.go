package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strconv"
	"syscall"

	"barfimanga/internal/config"
	"barfimanga/internal/core"
	"barfimanga/internal/tui"
)

type cliOptions struct {
	ConfigMode  string
	Interactive bool
	Directory   string
	Workers     int
	Host        string
	Quiet       bool
	Recursive   bool
	Retry       int
	Token       string
	RateLimit   float64
	Group       string
	MangaID     string
	GitHubFolder string
	UseRoot      bool
	ForceRebuild bool
	SyncOnly     bool
	MangaEntry   config.MangaEntry
}

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

// applyPrecedence aplica valores de Flags e Env Vars na Configuração carregada
func applyPrecedence(opts *cliOptions, cfg *config.Config) {
	// Env Vars
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

	// CLI Flags (Maior Precedência)
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

func main() {
	opts, err := parseFlags(os.Args[1:])
	if err != nil {
		if err == flag.ErrHelp {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "Erro ao parsear flags: %v\n", err)
		os.Exit(2)
	}

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

	mCfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Erro ao carregar configuração: %v\n", err)
		os.Exit(1)
	}

	// Aplica precedência (Flags > Env > Config)
	active := mCfg.GetActive()
	applyPrecedence(opts, &active)
	mCfg.Profiles[mCfg.ActiveProfile] = active // Salva de volta no struct em memória

	// TUI Padrão se nenhuma pasta e nem config for passada
	if opts.Directory == "" && opts.ConfigMode == "" {
		opts.Interactive = true
	}

	for {
		if opts.Interactive {
			dir, mangaID, ghFolder, forceRebuild, syncOnly, entry, err := tui.RunInteractive(&mCfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Erro no modo interativo: %v\n", err)
				os.Exit(1)
			}
			if dir != "" {
				opts.Directory = dir
				opts.MangaID = mangaID
				opts.GitHubFolder = ghFolder
				opts.ForceRebuild = forceRebuild
				opts.SyncOnly = syncOnly
				opts.MangaEntry = entry
				if ghFolder == "" {
					opts.UseRoot = true
				}
			} else {
				return // Saiu de forma graciosa via Menu -> Sair
			}
		}
		
		if opts.Directory != "" {
			active := mCfg.GetActive()
			applyPrecedence(opts, &active)
			
			groupName := opts.Group
			if opts.Group == "Default" && active.ScanGroup != "" {
				groupName = active.ScanGroup
			}

			fmt.Printf("Iniciando processo para o diretório: %s\n", opts.Directory)
			
			ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			pipeline, err := core.NewPipeline(mCfg)
			if err != nil {
				fmt.Fprintf(os.Stderr, "Erro ao configurar pipeline: %v\n", err)
				if opts.Interactive { continue } else { os.Exit(1) }
			}

			if err := pipeline.Run(ctx, opts.Directory, opts.Quiet, groupName, opts.MangaID, opts.GitHubFolder, opts.UseRoot, opts.ForceRebuild, opts.MangaEntry, opts.SyncOnly); err != nil {
				fmt.Fprintf(os.Stderr, "Erro crítico na execução: %v\n", err)
			}
			
			if opts.Interactive {
				fmt.Println("\n[!] Trabalho concluído. Retornando ao menu...")
				// Limpa as flags do diretório para a próxima iteração do loop
				opts.Directory = ""
				continue 
			}
			return
		} else {
			if !opts.Interactive {
				fmt.Println("Use -h ou --help para ver as opções.")
			}
			return
		}
	}
}
