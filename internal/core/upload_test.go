package core

import (
	"archive/zip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"barfimanga/internal/config"
	"barfimanga/internal/github"
	"barfimanga/internal/models"
	"barfimanga/internal/utils"
)

func TestChapterKeyAceitaNumeroPrimeiroOuComPrefixo(t *testing.T) {
	cases := []struct {
		folder string
		want   string
	}{
		{"65 - O Poço Oculto e a Luz da Lua (2)", "065"},
		{"66 - O Poço Oculto e a Luz da Lua (3)", "066"},
		{"Cap 019.1 - Título", "019.1"},
		{"Cap 037 - Título", "037"},
		{"Cap 000", "000"},
		{"Ch.001 - O Andar de Testes", "001"},
		{"Ch.010 - Rainha dos Golfinhos de Rede", "010"},
		{"Ch.068 - O líder da família Ari", "068"},
		{"Ch.65,5 - Título", "065.5"},
		{"Cap 70,25 - Título", "070.25"},
	}

	seen := make(map[string]string)
	for _, c := range cases {
		got := chapterKey(c.folder)
		if got != c.want {
			t.Errorf("chapterKey(%q) = %q, esperado %q", c.folder, got, c.want)
		}
		if prev, ok := seen[got]; ok {
			t.Errorf("colisão de chave: %q e %q geraram a mesma chave %q", prev, c.folder, got)
		}
		seen[got] = c.folder
	}
}

// TestChapterKeyVirgulaEPontoSaoCompativeis garante que ponto e vírgula como
// separador decimal são tratados como o MESMO capítulo (mesma chave) — não
// dá pra checar isso na lista de colisão acima porque aqui a colisão é
// esperada e correta.
func TestChapterKeyVirgulaEPontoSaoCompativeis(t *testing.T) {
	pares := [][2]string{
		{"65.5 - Título", "65,5 - Título"},
		{"Ch.065.5 - Título", "Ch.065,5 - Título"},
		{"Cap 19.1 - Título", "Cap 19,1 - Título"},
	}
	for _, p := range pares {
		a, b := chapterKey(p[0]), chapterKey(p[1])
		if a != b {
			t.Errorf("chapterKey(%q)=%q e chapterKey(%q)=%q deveriam ser iguais", p[0], a, p[1], b)
		}
	}
}

func TestChapterNumberFromNameAceitaNumeroPrimeiroOuComPrefixo(t *testing.T) {
	cases := []struct {
		folder string
		want   float64
		ok     bool
	}{
		{"65 - O Poço Oculto e a Luz da Lua (2)", 65, true},
		{"Cap 019.1 - Título", 19.1, true},
		{"Extra", 0, false},
	}

	for _, c := range cases {
		got, ok := chapterNumberFromName(c.folder)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("chapterNumberFromName(%q) = (%v, %v), esperado (%v, %v)", c.folder, got, ok, c.want, c.ok)
		}
	}
}

// fakeHost simula um Host sem tocar na rede — sempre reporta sucesso.
type fakeHost struct{}

func (fakeHost) Name() string { return "fake" }

func (fakeHost) UploadImage(ctx context.Context, path string) (models.UploadResult, error) {
	return models.UploadResult{URL: "https://fake.test/" + filepath.Base(path), Filename: filepath.Base(path), Success: true}, nil
}

func (fakeHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}

// TestRunProcessaPastaEArchiveNoMesmoLote garante que um capítulo em pasta e
// um capítulo empacotado num .cbz (extraído sob demanda) convivem no mesmo
// upload e viram entradas corretas no reader.json.
func TestRunProcessaPastaEArchiveNoMesmoLote(t *testing.T) {
	mangaRoot := t.TempDir()

	folderCh := filepath.Join(mangaRoot, "Cap 001 - Primeiro")
	if err := os.MkdirAll(folderCh, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folderCh, "01.jpg"), []byte("pagina"), 0644); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(mangaRoot, "Cap 002 - Segundo.cbz")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	fw, err := zw.Create("01.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("pagina do zip")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	zf.Close()

	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	active := config.Config{Workers: 2, RateLimit: 0, MaxRetries: 1}
	p := &Pipeline{
		active: active,
		host:   fakeHost{},
		client: github.NewClient(active), // GitHubToken vazio -> uploadToGitHub só avisa e retorna nil
	}

	err = p.Run(context.Background(), mangaRoot, true, "TestGroup", "obra_teste", "", true, false, config.MangaEntry{}, false)
	if err != nil {
		t.Fatalf("Run retornou erro: %v", err)
	}

	jsonPath := filepath.Join(mangaRoot, "obra_teste.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatalf("reader.json não foi criado: %v", err)
	}

	for _, want := range []string{`"001"`, `"002"`, "fake.test/01.jpg"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("esperava %q no reader.json, veio:\n%s", want, string(data))
		}
	}
}

// TestRunAvisaImagemSoltaIgnoradaComArchiveNaRaiz documenta um caso limite
// real: se tiver imagem solta direto na raiz da obra E também um arquivo de
// capítulo (.cbz/.zip/...) no mesmo diretório, a imagem solta não vira
// capítulo nenhum (o auto-fill de capítulo único não dispara, já que achou
// um capítulo de verdade) — ela é descartada. O comportamento em si não é
// resolvido aqui (não daria pra saber que "número" ela deveria ter), mas o
// usuário precisa ser avisado em vez de simplesmente perder a página em
// silêncio.
func TestRunAvisaImagemSoltaIgnoradaComArchiveNaRaiz(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "01.jpg"), []byte("pagina solta"), 0644); err != nil {
		t.Fatal(err)
	}

	zipPath := filepath.Join(root, "Cap 002.cbz")
	zf, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(zf)
	fw, err := zw.Create("01.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("pagina do zip")); err != nil {
		t.Fatal(err)
	}
	zw.Close()
	zf.Close()

	oldwd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	oldStderr := os.Stderr
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stderr = w

	active := config.Config{Workers: 1, MaxRetries: 1}
	p := &Pipeline{active: active, host: fakeHost{}, client: github.NewClient(active)}
	runErr := p.Run(context.Background(), root, true, "G", "obra", "", true, false, config.MangaEntry{}, false)

	w.Close()
	os.Stderr = oldStderr
	stderrBytes, _ := io.ReadAll(r)
	stderr := string(stderrBytes)

	if runErr != nil {
		t.Fatalf("Run retornou erro: %v", runErr)
	}
	if !strings.Contains(stderr, "imagem(ns) solta") {
		t.Fatalf("esperava aviso sobre imagem solta ignorada no stderr, veio:\n%s", stderr)
	}

	jsonPath := filepath.Join(root, "obra.json")
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"002"`) {
		t.Errorf("esperava o capítulo do .cbz no reader.json, veio:\n%s", string(data))
	}
}

// quotaHost simula um host que aceita só N uploads antes de começar a
// recusar (como o "Upload limit reached" real do ImgChest no meio de um
// capítulo).
type quotaHost struct {
	mu        sync.Mutex
	remaining int
}

func (h *quotaHost) Name() string { return "quota" }

func (h *quotaHost) UploadImage(ctx context.Context, path string) (models.UploadResult, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.remaining <= 0 {
		return models.UploadResult{Filename: filepath.Base(path), Success: false, Error: "quota excedida"}, fmt.Errorf("quota excedida")
	}
	h.remaining--
	return models.UploadResult{URL: "https://fake.test/" + filepath.Base(path), Filename: filepath.Base(path), Success: true}, nil
}

func (h *quotaHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}

// TestRunCapituloParcialNaoFicaMarcadoComoCompletoEDepoisSeCompleta reproduz
// o cenário real de bater numa cota de upload no meio de um capítulo: antes
// do fix, um capítulo com sucesso parcial (2 de 3 imagens) era marcado como
// concluído no checkpoint, perdendo a imagem que faltou pra sempre. Agora
// não marca — a segunda execução (simulando a cota liberada depois) reusa
// do cache as que já subiram e só reenvia a que faltou, completando o
// capítulo.
func TestRunCapituloParcialNaoFicaMarcadoComoCompletoEDepoisSeCompleta(t *testing.T) {
	mangaRoot := t.TempDir()
	chDir := filepath.Join(mangaRoot, "Ch.021 - Erupção")
	if err := os.MkdirAll(chDir, 0755); err != nil {
		t.Fatal(err)
	}
	for i, content := range []string{"pagina1", "pagina2", "pagina3"} {
		name := fmt.Sprintf("%02d.jpg", i+1)
		if err := os.WriteFile(filepath.Join(chDir, name), []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
	}

	oldwd, _ := os.Getwd()
	if err := os.Chdir(t.TempDir()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	active := config.Config{Workers: 1, MaxRetries: 1}
	host := &quotaHost{remaining: 2} // só deixa 2 das 3 imagens passarem
	p := &Pipeline{active: active, host: host, client: github.NewClient(active)}

	jsonPath := filepath.Join(mangaRoot, "obra.json")

	// 1ª execução: bate na cota no meio do capítulo.
	if err := p.Run(context.Background(), mangaRoot, true, "G", "obra", "", true, false, config.MangaEntry{}, false); err != nil {
		t.Fatalf("1ª execução: Run retornou erro: %v", err)
	}
	data, err := os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "fake.test/") != 2 {
		t.Fatalf("esperava 2 imagens salvas após a 1ª execução, veio:\n%s", string(data))
	}

	state := loadStateForTest(t, mangaRoot)
	if state.CompletedChapters["Ch.021 - Erupção"] {
		t.Fatal("capítulo parcial não deveria estar marcado como concluído no checkpoint")
	}

	// "Cota libera" — segunda execução deve completar a imagem que faltou.
	host.remaining = 10
	if err := p.Run(context.Background(), mangaRoot, true, "G", "obra", "", true, false, config.MangaEntry{}, false); err != nil {
		t.Fatalf("2ª execução: Run retornou erro: %v", err)
	}
	data, err = os.ReadFile(jsonPath)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(data), "fake.test/") != 3 {
		t.Fatalf("esperava as 3 imagens salvas após a 2ª execução, veio:\n%s", string(data))
	}

	state = loadStateForTest(t, mangaRoot)
	if !state.CompletedChapters["Ch.021 - Erupção"] {
		t.Fatal("capítulo deveria estar marcado como concluído depois de completar na 2ª execução")
	}
}

func loadStateForTest(t *testing.T, dbRoot string) *utils.UploadState {
	t.Helper()
	return utils.LoadState(dbRoot)
}
