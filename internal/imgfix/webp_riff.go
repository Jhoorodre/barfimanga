package imgfix

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// riffChunk é um pedaço bruto do container RIFF/WEBP: tag de 4 bytes +
// payload (sem o byte de padding, se houver).
type riffChunk struct {
	tag  string
	data []byte
}

func parseWebPChunks(path string) ([]riffChunk, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) < 12 || string(raw[0:4]) != "RIFF" || string(raw[8:12]) != "WEBP" {
		return nil, fmt.Errorf("não é um container RIFF/WEBP válido")
	}

	var chunks []riffChunk
	body := raw[12:]
	i := 0
	for i+8 <= len(body) {
		tag := string(body[i : i+4])
		size := binary.LittleEndian.Uint32(body[i+4 : i+8])
		start := i + 8
		end := start + int(size)
		if end > len(body) {
			return nil, fmt.Errorf("chunk %q corrompido: tamanho declarado excede o arquivo", tag)
		}
		chunks = append(chunks, riffChunk{tag: tag, data: body[start:end]})
		i = end
		if size%2 == 1 {
			i++ // padding pra manter alinhamento par
		}
	}
	return chunks, nil
}

func writeWebPChunks(chunks []riffChunk) []byte {
	var body bytes.Buffer
	body.WriteString("WEBP")
	for _, c := range chunks {
		body.WriteString(c.tag)
		size := make([]byte, 4)
		binary.LittleEndian.PutUint32(size, uint32(len(c.data)))
		body.Write(size)
		body.Write(c.data)
		if len(c.data)%2 == 1 {
			body.WriteByte(0)
		}
	}

	var out bytes.Buffer
	out.WriteString("RIFF")
	riffSize := make([]byte, 4)
	binary.LittleEndian.PutUint32(riffSize, uint32(body.Len()))
	out.Write(riffSize)
	out.Write(body.Bytes())
	return out.Bytes()
}

// SimplifyWebP tenta remover o container VP8X estendido (e metadados como
// ICCP/EXIF/XMP) de um .webp SEM recodificar — reempacota o(s) chunk(s) de
// imagem originais bit a bit. Alguns hosts (ex: ImgChest) rejeitam WebP
// estendido mesmo sendo válido; isso resolve o caso comum sem nenhuma perda
// de qualidade, ao contrário do fallback JPEG (NormalizeWebP).
//
// applicable=false quando o arquivo é animado (ANIM/ANMF) — nesses casos
// simplificar exigiria decisões que este código não toma, e o chamador deve
// cair pro fallback com perdas. Imagem lossless (VP8L) é sempre simplificável
// porque o alpha, quando existe, já vive dentro do próprio bitstream VP8L,
// não depende do container estendido.
func SimplifyWebP(path string) (newPath string, cleanup func(), applicable bool, err error) {
	chunks, err := parseWebPChunks(path)
	if err != nil {
		return "", nil, false, err
	}
	if len(chunks) == 0 || chunks[0].tag != "VP8X" {
		return "", nil, false, nil // já é formato simples, nada a fazer
	}

	var core, alpha *riffChunk
	for i := range chunks {
		switch chunks[i].tag {
		case "VP8L":
			core = &chunks[i]
		case "VP8 ":
			if core == nil || core.tag != "VP8L" {
				core = &chunks[i]
			}
		case "ALPH":
			alpha = &chunks[i]
		case "ANIM", "ANMF":
			return "", nil, false, nil // animado — não dá pra simplificar aqui
		}
	}
	if core == nil {
		return "", nil, false, fmt.Errorf("chunk de imagem (VP8/VP8L) não encontrado")
	}

	var newChunks []riffChunk
	if core.tag == "VP8L" {
		// VP8L carrega alpha no próprio bitstream — nunca precisa do VP8X.
		newChunks = []riffChunk{*core}
	} else if alpha != nil {
		// VP8 lossy com alpha separado: o formato simples não tem onde por
		// o ALPH, então mantém um VP8X mínimo (só a flag de alpha) + ALPH —
		// ainda remove ICCP/EXIF/XMP, que é o que geralmente confunde o
		// validador do host.
		vp8x := riffChunk{tag: "VP8X", data: []byte{0x10, 0, 0, 0, 0, 0, 0, 0, 0, 0}}
		newChunks = []riffChunk{vp8x, *alpha, *core}
	} else {
		newChunks = []riffChunk{*core}
	}

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	tmp, err := os.CreateTemp("", base+"-simple-*.webp")
	if err != nil {
		return "", nil, false, err
	}
	if _, err := tmp.Write(writeWebPChunks(newChunks)); err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return "", nil, false, err
	}
	if err := tmp.Close(); err != nil {
		os.Remove(tmp.Name())
		return "", nil, false, err
	}

	return tmp.Name(), func() { os.Remove(tmp.Name()) }, true, nil
}
