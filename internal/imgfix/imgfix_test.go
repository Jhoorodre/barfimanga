package imgfix

import (
	"bytes"
	"image/png"
	"os"
	"testing"
)

// TestNormalizeWebPDecodificaEstendidoEGeraPNGValido usa um .webp real no
// formato estendido (VP8X, com ICC profile embutido) — o mesmo formato que o
// ImgChest rejeita mesmo sendo válido (ver worker.Pool) — e confirma que
// vira um PNG decodificável, num arquivo novo que existe de verdade.
func TestNormalizeWebPDecodificaEstendidoEGeraPNGValido(t *testing.T) {
	newPath, cleanup, err := NormalizeWebP("testdata/extended-with-alpha.webp")
	if err != nil {
		t.Fatalf("NormalizeWebP retornou erro: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("arquivo de saída não existe: %v", err)
	}
	if _, err := png.Decode(bytes.NewReader(data)); err != nil {
		t.Fatalf("saída não é um PNG válido: %v", err)
	}
}

// TestNormalizeWebPCleanupRemoveArquivo garante que cleanup() de fato apaga
// o temporário — senão runs longos acumulam lixo em /tmp.
func TestNormalizeWebPCleanupRemoveArquivo(t *testing.T) {
	newPath, cleanup, err := NormalizeWebP("testdata/extended-with-alpha.webp")
	if err != nil {
		t.Fatalf("NormalizeWebP retornou erro: %v", err)
	}
	cleanup()
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("esperava que cleanup() removesse %q, mas ainda existe", newPath)
	}
}

// TestNormalizeWebPArquivoInvalidoDevolveErro garante que um arquivo que não
// é webp de verdade falha explicitamente, em vez de gerar um PNG lixo.
func TestNormalizeWebPArquivoInvalidoDevolveErro(t *testing.T) {
	tmp, err := os.CreateTemp(t.TempDir(), "nao-e-webp-*.webp")
	if err != nil {
		t.Fatal(err)
	}
	tmp.WriteString("isso nao e um webp")
	tmp.Close()

	if _, _, err := NormalizeWebP(tmp.Name()); err == nil {
		t.Fatal("esperava erro pra arquivo que não é webp válido")
	}
}
