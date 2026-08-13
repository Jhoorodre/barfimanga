package hosts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"barfimanga/internal/config"
)

// TestImgChestUploadRespeitaRetryAfter reproduz o "Upload limit reached"
// real do ImgChest: confirma que UploadImage dorme pelo menos retry_after
// segundos antes de devolver o erro, em vez de falhar na hora.
func TestImgChestUploadRespeitaRetryAfter(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"Upload limit reached. Try again in 55 minutes.","remaining":0,"retry_after":1}`))
	}))
	defer server.Close()

	h := NewImgChestHost(config.Config{HostToken: "fake-token"})
	h.client = &http.Client{Transport: redirectTransport{target: server.URL}}

	start := time.Now()
	_, err := h.UploadImage(context.Background(), writeTempImage(t))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("esperava erro (429)")
	}
	if elapsed < 1*time.Second {
		t.Fatalf("esperava esperar pelo menos 1s (retry_after), levou %v", elapsed)
	}
}

// TestImgChestUploadSemRetryAfterNaoEspera garante que o 429 genérico do
// throttle (sem retry_after no corpo) não trava a chamada.
func TestImgChestUploadSemRetryAfterNaoEspera(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"message":"Too Many Attempts."}`))
	}))
	defer server.Close()

	h := NewImgChestHost(config.Config{HostToken: "fake-token"})
	h.client = &http.Client{Transport: redirectTransport{target: server.URL}}

	start := time.Now()
	_, err := h.UploadImage(context.Background(), writeTempImage(t))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("esperava erro (429)")
	}
	if elapsed > 500*time.Millisecond {
		t.Fatalf("não deveria esperar sem retry_after no corpo, levou %v", elapsed)
	}
}

// TestImgChestUploadRetryAfterRespeitaCancelamento garante que cancelar o
// contexto interrompe a espera em vez de segurar o processo até o fim.
func TestImgChestUploadRetryAfterRespeitaCancelamento(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"error":"Upload limit reached.","remaining":0,"retry_after":3600}`))
	}))
	defer server.Close()

	h := NewImgChestHost(config.Config{HostToken: "fake-token"})
	h.client = &http.Client{Transport: redirectTransport{target: server.URL}}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := h.UploadImage(ctx, writeTempImage(t))
	elapsed := time.Since(start)

	if err == nil {
		t.Fatal("esperava erro")
	}
	if elapsed > 2*time.Second {
		t.Fatalf("cancelamento do contexto deveria interromper a espera de 1h, levou %v", elapsed)
	}
}
