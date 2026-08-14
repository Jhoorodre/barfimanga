// Package imgfix contém correções pontuais de imagem usadas como fallback de
// upload quando um host rejeita um arquivo por causa do formato.
package imgfix

import (
	"bytes"
	"fmt"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/image/webp"
)

// jpegQuality é a qualidade usada ao reconverter — alta o bastante pra não
// introduzir artefato visível em página de mangá, baixa o bastante pra não
// perder a vantagem de tamanho sobre o WebP original.
const jpegQuality = 92

// NormalizeWebP decodifica um .webp e regrava como JPEG num arquivo
// temporário. Existe porque alguns hosts (ex: ImgChest) rejeitam WebP
// estendido (VP8X — usado quando o arquivo tem canal alpha ou perfil ICC
// embutido) com erro de "tipo de arquivo inválido", mesmo sendo um .webp
// válido pela spec. JPEG foi escolhido em vez de PNG por ser MUITO menor
// (relevante já que o Go não tem encoder de WebP na stdlib nem no x/image —
// CGO/libwebp quebraria o binário estático, e depender de um binário externo
// tipo cwebp não é portável pro .exe do Windows).
//
// Retorna o path do JPEG gerado e uma função de limpeza que o chamador deve
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
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: jpegQuality}); err != nil {
		return "", nil, fmt.Errorf("codificar jpeg: %w", err)
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	tmp, err := os.CreateTemp("", base+"-*.jpg")
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
