package hosts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"barfimanga/internal/config"
)

// TestSxcuUploadSucesso reproduz a resposta real observada testando contra a
// API de verdade: {"id","url","del_url","thumb"}, sem exigir token.
func TestSxcuUploadSucesso(t *testing.T) {
	var gotNoembed, gotUserAgent string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUserAgent = r.Header.Get("User-Agent")
		if err := r.ParseMultipartForm(1 << 20); err != nil {
			t.Fatal(err)
		}
		gotNoembed = r.FormValue("noembed")
		w.Write([]byte(`{"id":"7N1AA18gO","url":"https://sxcu.net/7N1AA18gO.jpeg","del_url":"https://sxcu.net/api/files/delete/7N1AA18gO/x","thumb":"https://sxcu.net/t/7N1AA18gO.jpeg"}`))
	}))
	defer server.Close()

	h := NewSxcuHost(config.Config{})
	h.client = &http.Client{Transport: redirectTransport{target: server.URL}}

	res, err := h.UploadImage(context.Background(), writeTempImage(t))
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != "https://sxcu.net/7N1AA18gO.jpeg" || !res.Success {
		t.Fatalf("resultado inesperado: %+v", res)
	}
	if gotNoembed == "" {
		t.Fatal("esperava o campo noembed presente no upload (senão a API devolve link de página, não de arquivo)")
	}
	if gotUserAgent == "" {
		t.Fatal("esperava um User-Agent não vazio (a API rejeita sem isso)")
	}
}

// TestSxcuUploadErro garante que erro da API (ex: rate limit) vira erro Go
// com a mensagem visível, não sucesso silencioso.
func TestSxcuUploadErro(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"Rate limit exceeded","code":185}`))
	}))
	defer server.Close()

	h := NewSxcuHost(config.Config{})
	h.client = &http.Client{Transport: redirectTransport{target: server.URL}}

	res, err := h.UploadImage(context.Background(), writeTempImage(t))
	if err == nil {
		t.Fatal("esperava erro")
	}
	if res.Success {
		t.Fatal("esperava Success=false")
	}
	if !strings.Contains(err.Error(), "429") {
		t.Fatalf("esperava o status 429 na mensagem de erro, veio: %v", err)
	}
}

// TestSxcuRespeitaRateLimitHeaderEAcumulaQuotaCycles reproduz o 429 real da
// API (rate limit de 3 req/min) com o header X-RateLimit-Reset-After, e
// garante que o upload espera esse tempo e contabiliza o ciclo.
func TestSxcuRespeitaRateLimitHeaderEAcumulaQuotaCycles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-RateLimit-Reset-After", "1")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"Rate limit exceeded","code":"815"}`))
	}))
	defer server.Close()

	h := NewSxcuHost(config.Config{})
	h.client = &http.Client{Transport: redirectTransport{target: server.URL}}

	start := time.Now()
	_, err := h.UploadImage(context.Background(), writeTempImage(t))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("esperava erro (429)")
	}
	if elapsed < 1*time.Second {
		t.Fatalf("esperava esperar pelo menos 1s (X-RateLimit-Reset-After), levou %v", elapsed)
	}

	waits, total := h.QuotaCycles()
	if waits != 1 {
		t.Fatalf("esperava 1 ciclo de rate-limit, veio %d", waits)
	}
	if total < 1*time.Second {
		t.Fatalf("esperava pelo menos 1s de espera acumulada, veio %v", total)
	}
}

// TestSxcuSemHeaderRateLimitNaoEspera garante que um 429 sem o header não
// trava a chamada (comportamento antigo preservado).
func TestSxcuSemHeaderRateLimitNaoEspera(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"Global rate limit exceeded","code":"02"}`))
	}))
	defer server.Close()

	h := NewSxcuHost(config.Config{})
	h.client = &http.Client{Transport: redirectTransport{target: server.URL}}

	start := time.Now()
	_, err := h.UploadImage(context.Background(), writeTempImage(t))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("esperava erro (429)")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("não deveria esperar sem o header, levou %v", elapsed)
	}
	if waits, _ := h.QuotaCycles(); waits != 0 {
		t.Fatalf("esperava zero ciclos sem o header, veio %d", waits)
	}
}
