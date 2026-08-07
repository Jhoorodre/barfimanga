// Package archive extrai arquivos compactados de mangá (.zip/.cbz/.rar/.cbr)
// para uma pasta temporária, permitindo que o pipeline trate um arquivo solto
// na raiz da obra como se fosse uma pasta de capítulo comum.
package archive

import (
	"archive/zip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/nwaples/rardecode/v2"
)

// IsArchive diz se o nome do arquivo tem uma extensão suportada.
func IsArchive(name string) bool {
	switch strings.ToLower(filepath.Ext(name)) {
	case ".zip", ".cbz", ".rar", ".cbr":
		return true
	}
	return false
}

// Extract descompacta path em destDir. A estrutura interna é achatada — toda
// entrada vira um arquivo solto na raiz de destDir, ignorando subpastas do
// archive (páginas de mangá quase sempre já vêm soltas na raiz mesmo).
func Extract(path, destDir string) error {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".zip", ".cbz":
		return extractZip(path, destDir)
	case ".rar", ".cbr":
		return extractRar(path, destDir)
	default:
		return fmt.Errorf("archive: formato não suportado: %s", path)
	}
}

func extractZip(path, destDir string) error {
	r, err := zip.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()

	for _, f := range r.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return fmt.Errorf("abrir %s no zip: %w", f.Name, err)
		}
		err = writeFlat(destDir, f.Name, rc)
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

func extractRar(path, destDir string) error {
	r, err := rardecode.OpenReader(path)
	if err != nil {
		return err
	}
	defer r.Close()

	for {
		header, err := r.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		if header.IsDir {
			continue
		}
		if err := writeFlat(destDir, header.Name, r); err != nil {
			return err
		}
	}
	return nil
}

// writeFlat grava src em destDir usando só o nome-base da entrada — ignora
// qualquer subpasta interna do archive, o que também evita path traversal
// (uma entrada tipo "../../etc/passwd" vira só "passwd").
func writeFlat(destDir, entryName string, src io.Reader) error {
	name := filepath.Base(filepath.FromSlash(entryName))
	if name == "" || name == "." || name == string(filepath.Separator) {
		return nil
	}

	dest := dedupe(filepath.Join(destDir, name))
	out, err := os.Create(dest)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, src)
	return err
}

// dedupe evita sobrescrever se duas entradas do archive caírem no mesmo
// nome-base depois de achatado (raro, mas pode acontecer com subpastas).
func dedupe(path string) string {
	if _, err := os.Stat(path); err != nil {
		return path
	}
	ext := filepath.Ext(path)
	base := strings.TrimSuffix(path, ext)
	for i := 1; ; i++ {
		candidate := base + "_" + strconv.Itoa(i) + ext
		if _, err := os.Stat(candidate); err != nil {
			return candidate
		}
	}
}
