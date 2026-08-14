// Package imgfix contém correções pontuais de imagem usadas como fallback de
// upload quando um host rejeita um arquivo por causa do formato.
package imgfix

import (
	"bytes"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/webp"
)

// NormalizeWebP decodifica um .webp e regrava como PNG num arquivo
// temporário. Existe porque alguns hosts (ex: ImgChest) rejeitam WebP
// estendido (VP8X — usado quando o arquivo tem canal alpha ou perfil ICC
// embutido) com erro de "tipo de arquivo inválido", mesmo sendo um .webp
// válido pela spec. PNG evita essa ambiguidade e preserva alpha sem perdas.
//
// Retorna o path do PNG gerado e uma função de limpeza que o chamador deve
// invocar depois do upload.
func NormalizeWebP(path string) (newPath string, cleanup func(), err error) {
	f, err := os.Open(path)
	if err != nil {
		return "", nil, err
	}
	defer f.Close()

	img, err := webp.Decode(f)
	if err != nil {
		return "", nil, fmt.Errorf("decodificar webp: %w", err)
	}

	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		return "", nil, fmt.Errorf("codificar png: %w", err)
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	tmp, err := os.CreateTemp("", base+"-*.png")
	if err != nil {
		return "", nil, err
	}
	if _, err := tmp.Write(buf.Bytes()); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", nil, err
	}

	return tmp.Name(), func() { os.Remove(tmp.Name()) }, nil
}
