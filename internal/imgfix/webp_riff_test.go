package imgfix

import (
	"bytes"
	"encoding/binary"
	"image"
	"os"
	"testing"

	"golang.org/x/image/webp"
)

func decodeFile(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("abrir %q: %v", path, err)
	}
	defer f.Close()
	img, err := webp.Decode(f)
	if err != nil {
		t.Fatalf("decodificar %q: %v", path, err)
	}
	return img
}

// TestSimplifyWebPVP8LSempreSimplificaSemPerda cobre o caso comum de mangá:
// VP8X estendido (com metadado tipo EXIF) envolvendo um VP8L — o alpha, se
// existir, já mora dentro do bitstream VP8L, então dá pra descartar o VP8X e
// os metadados sem alterar um pixel sequer.
func TestSimplifyWebPVP8LSempreSimplificaSemPerda(t *testing.T) {
	newPath, cleanup, applicable, err := SimplifyWebP("testdata/extended-vp8l-no-alpha.webp")
	if err != nil {
		t.Fatalf("SimplifyWebP retornou erro: %v", err)
	}
	if !applicable {
		t.Fatal("esperava applicable=true pra um VP8X envolvendo VP8L")
	}
	defer cleanup()

	before := decodeFile(t, "testdata/extended-vp8l-no-alpha.webp")
	after := decodeFile(t, newPath)

	b := before.Bounds()
	if b != after.Bounds() {
		t.Fatalf("dimensões mudaram: antes=%v depois=%v", b, after.Bounds())
	}
	for y := b.Min.Y; y < b.Max.Y; y++ {
		for x := b.Min.X; x < b.Max.X; x++ {
			if before.At(x, y) != after.At(x, y) {
				t.Fatalf("pixel (%d,%d) mudou: antes=%v depois=%v", x, y, before.At(x, y), after.At(x, y))
			}
		}
	}

	// confirma que o resultado é mesmo formato simples (sem VP8X)
	chunks, err := parseWebPChunks(newPath)
	if err != nil {
		t.Fatal(err)
	}
	if len(chunks) != 1 || chunks[0].tag != "VP8L" {
		t.Fatalf("esperava só um chunk VP8L no resultado, veio: %v", chunkTags(chunks))
	}
}

// TestSimplifyWebPLossyComAlphaMantemALPH garante que uma imagem VP8 (lossy)
// com canal alpha de verdade (chunk ALPH separado) não perde o alpha —
// mantém um VP8X mínimo + ALPH, só removendo o que não afeta a imagem.
func TestSimplifyWebPLossyComAlphaMantemALPH(t *testing.T) {
	newPath, cleanup, applicable, err := SimplifyWebP("testdata/lossy-with-alpha.webp")
	if err != nil {
		t.Fatalf("SimplifyWebP retornou erro: %v", err)
	}
	if !applicable {
		t.Fatal("esperava applicable=true")
	}
	defer cleanup()

	// decodifica só pra garantir que continua um webp válido e íntegro
	decodeFile(t, newPath)

	chunks, err := parseWebPChunks(newPath)
	if err != nil {
		t.Fatal(err)
	}
	hasAlpha := false
	for _, c := range chunks {
		if c.tag == "ALPH" {
			hasAlpha = true
		}
	}
	if !hasAlpha {
		t.Fatal("esperava que o chunk ALPH fosse preservado")
	}
}

// TestSimplifyWebPAnimadoNaoAplicavel garante que um webp animado (ANIM) não
// é mexido — retorna applicable=false, deixando o chamador cair pro
// fallback com perdas.
func TestSimplifyWebPAnimadoNaoAplicavel(t *testing.T) {
	tmp := t.TempDir() + "/fake-anim.webp"
	writeFakeAnimatedWebP(t, tmp)

	_, _, applicable, err := SimplifyWebP(tmp)
	if err != nil {
		t.Fatalf("não esperava erro, veio: %v", err)
	}
	if applicable {
		t.Fatal("esperava applicable=false pra um webp animado")
	}
}

// TestSimplifyWebPFormatoJaSimplesNaoAplicavel garante que um .webp que já é
// simples (sem VP8X) não gera nenhum arquivo novo à toa.
func TestSimplifyWebPFormatoJaSimplesNaoAplicavel(t *testing.T) {
	_, _, applicable, err := SimplifyWebP("../imgfix/testdata/extended-vp8l-no-alpha.webp")
	if err != nil {
		t.Fatal(err)
	}
	_ = applicable // sanity: função acima já cobre o caso "true"; aqui cobrimos "já simples"

	simplePath := t.TempDir() + "/ja-simples.webp"
	chunks, err := parseWebPChunks("testdata/extended-vp8l-no-alpha.webp")
	if err != nil {
		t.Fatal(err)
	}
	var vp8l riffChunk
	for _, c := range chunks {
		if c.tag == "VP8L" {
			vp8l = c
		}
	}
	if err := os.WriteFile(simplePath, writeWebPChunks([]riffChunk{vp8l}), 0644); err != nil {
		t.Fatal(err)
	}

	_, _, applicable2, err := SimplifyWebP(simplePath)
	if err != nil {
		t.Fatal(err)
	}
	if applicable2 {
		t.Fatal("esperava applicable=false pra um webp que já é simples (sem VP8X)")
	}
}

func chunkTags(chunks []riffChunk) []string {
	tags := make([]string, len(chunks))
	for i, c := range chunks {
		tags[i] = c.tag
	}
	return tags
}

// writeFakeAnimatedWebP escreve um RIFF/WEBP com VP8X+ANIM+ANMF só com tags
// corretas (payloads fake) — o suficiente pra SimplifyWebP reconhecer que é
// animado sem precisar de um webp animado de verdade decodificável.
func writeFakeAnimatedWebP(t *testing.T, path string) {
	t.Helper()
	writeChunk := func(buf *bytes.Buffer, tag string, payload []byte) {
		buf.WriteString(tag)
		size := make([]byte, 4)
		binary.LittleEndian.PutUint32(size, uint32(len(payload)))
		buf.Write(size)
		buf.Write(payload)
		if len(payload)%2 == 1 {
			buf.WriteByte(0)
		}
	}

	var body bytes.Buffer
	body.WriteString("WEBP")
	writeChunk(&body, "VP8X", make([]byte, 10))
	writeChunk(&body, "ANIM", make([]byte, 6))
	writeChunk(&body, "ANMF", make([]byte, 16))

	var out bytes.Buffer
	out.WriteString("RIFF")
	size := make([]byte, 4)
	binary.LittleEndian.PutUint32(size, uint32(body.Len()))
	out.Write(size)
	out.Write(body.Bytes())

	if err := os.WriteFile(path, out.Bytes(), 0644); err != nil {
		t.Fatal(err)
	}
}
