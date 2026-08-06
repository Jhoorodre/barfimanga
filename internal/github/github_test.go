package github

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"barfimanga/internal/config"
)

func testConfig() config.Config {
	return config.Config{
		GitHubToken:  "fake-token",
		GitHubRepo:   "owner/repo",
		GitHubBranch: "main",
		MaxRetries:   2,
	}
}

func TestUploadJSONRetrySucedeAposFalhaTransitoria(t *testing.T) {
	var puts int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.WriteHeader(http.StatusNotFound) // arquivo não existe ainda
			return
		}
		if atomic.AddInt32(&puts, 1) == 1 {
			w.WriteHeader(http.StatusInternalServerError) // 1ª tentativa: falha transitória
			return
		}
		w.WriteHeader(http.StatusCreated) // 2ª tentativa: sucesso
	}))
	defer server.Close()

	c := NewClient(testConfig())
	c.baseURL = server.URL
	c.client.Timeout = 0 // sem timeout de rede real nesse teste local

	if err := c.UploadJSON(context.Background(), "obra.json", []byte("{}"), "update"); err != nil {
		t.Fatalf("esperava sucesso após retry, veio erro: %v", err)
	}
	if got := atomic.LoadInt32(&puts); got != 2 {
		t.Fatalf("esperava 2 tentativas de PUT, veio %d", got)
	}
}

func TestUploadJSONRetryEsgotaEDevolveErro(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer server.Close()

	c := NewClient(testConfig())
	c.baseURL = server.URL

	err := c.UploadJSON(context.Background(), "obra.json", []byte("{}"), "update")
	if err == nil {
		t.Fatal("esperava erro depois de esgotar as tentativas")
	}
}
