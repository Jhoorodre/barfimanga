package archive

import (
	"archive/zip"
	"os"
	"path/filepath"
	"testing"
)

func TestIsArchive(t *testing.T) {
	cases := map[string]bool{
		"Cap 001.zip": true,
		"Cap 001.cbz": true,
		"Cap 001.rar": true,
		"Cap 001.cbr": true,
		"Cap 001.CBZ": true, // case-insensitive
		"Cap 001":     false,
		"pagina.jpg":  false,
	}
	for name, want := range cases {
		if got := IsArchive(name); got != want {
			t.Errorf("IsArchive(%q) = %v, esperado %v", name, got, want)
		}
	}
}

func writeTestZip(t *testing.T, path string, entries map[string]string) {
	t.Helper()
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	w := zip.NewWriter(f)
	for name, content := range entries {
		fw, err := w.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := fw.Write([]byte(content)); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestExtractZipAchataSubpastas(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "cap.cbz")
	writeTestZip(t, zipPath, map[string]string{
		"01.jpg":             "pagina 1",
		"sub/02.jpg":         "pagina 2",
		"ComicInfo.xml":      "<xml/>",
		"outra/pasta/03.jpg": "pagina 3",
	})

	destDir := t.TempDir()
	if err := Extract(zipPath, destDir); err != nil {
		t.Fatal(err)
	}

	for _, want := range []string{"01.jpg", "02.jpg", "03.jpg", "ComicInfo.xml"} {
		data, err := os.ReadFile(filepath.Join(destDir, want))
		if err != nil {
			t.Errorf("esperava %s extraído, erro: %v", want, err)
			continue
		}
		if len(data) == 0 {
			t.Errorf("%s extraído vazio", want)
		}
	}
}

func TestExtractZipDedupeNomeColidido(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "cap.cbz")
	writeTestZip(t, zipPath, map[string]string{
		"a/01.jpg": "primeira",
		"b/01.jpg": "segunda", // mesmo nome-base depois de achatar
	})

	destDir := t.TempDir()
	if err := Extract(zipPath, destDir); err != nil {
		t.Fatal(err)
	}

	entries, err := os.ReadDir(destDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 2 {
		t.Fatalf("esperava 2 arquivos extraídos (sem sobrescrita), veio %d", len(entries))
	}
}

func TestExtractFormatoNaoSuportado(t *testing.T) {
	if err := Extract("arquivo.txt", t.TempDir()); err == nil {
		t.Fatal("esperava erro pra extensão não suportada")
	}
}
