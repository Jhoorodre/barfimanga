package hosts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"

	"barfimanga/internal/config"
)

func writeTempImage(t *testing.T) string {
	t.Helper()
	f, err := os.CreateTemp(t.TempDir(), "*.jpg")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err := f.WriteString("fake image bytes"); err != nil {
		t.Fatal(err)
	}
	return f.Name()
}

// TestImgurUploadErroFormatoJSONAPI reproduz o formato de erro real observado
// batendo direto na API do Imgur hoje: {"errors":[{"code","detail"}]}, sem os
// campos "success"/"data" do formato antigo. Antes desse fix, isso virava uma
// mensagem de erro vazia (só líamos data.error, que não existe nesse shape).
func TestImgurUploadErroFormatoJSONAPI(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"errors":[{"id":"legacy-api-x","code":"429","status":"Too Many Requests","detail":"Too Many Requests"}]}`))
	}))
	defer server.Close()

	// A URL da API é fixa dentro de UploadImage, então redirecionamos via
	// Transport customizado em vez de apontar o client pro server.URL direto.
	h := NewImgurHost(config.Config{HostToken: "fake-client-id"})
	h.client = &http.Client{Transport: redirectTransport{target: server.URL}}

	res, err := h.UploadImage(context.Background(), writeTempImage(t))
	if err == nil {
		t.Fatal("esperava erro")
	}
	if res.Success {
		t.Fatal("esperava Success=false")
	}
	if !strings.Contains(err.Error(), "Too Many Requests") {
		t.Fatalf("esperava a mensagem real do erro (\"Too Many Requests\") no erro, veio: %v", err)
	}
}

// TestImgurUploadErroFormatoClassico garante que o formato antigo
// {success:false, data:{error}} continua funcionando também.
func TestImgurUploadErroFormatoClassico(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		w.Write([]byte(`{"data":{"error":"Client-ID inválido"},"success":false,"status":400}`))
	}))
	defer server.Close()

	h := NewImgurHost(config.Config{HostToken: "fake-client-id"})
	h.client = &http.Client{Transport: redirectTransport{target: server.URL}}

	_, err := h.UploadImage(context.Background(), writeTempImage(t))
	if err == nil {
		t.Fatal("esperava erro")
	}
	if !strings.Contains(err.Error(), "Client-ID inválido") {
		t.Fatalf("esperava a mensagem do formato clássico no erro, veio: %v", err)
	}
}

// TestImgurUploadSucesso garante o caminho feliz continua extraindo o link certo.
func TestImgurUploadSucesso(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if auth := r.Header.Get("Authorization"); auth != "Client-ID fake-client-id" {
			t.Errorf("Authorization header = %q, esperado \"Client-ID fake-client-id\"", auth)
		}
		w.Write([]byte(`{"data":{"link":"https://i.imgur.com/abc123.jpg"},"success":true,"status":200}`))
	}))
	defer server.Close()

	h := NewImgurHost(config.Config{HostToken: "fake-client-id"})
	h.client = &http.Client{Transport: redirectTransport{target: server.URL}}

	res, err := h.UploadImage(context.Background(), writeTempImage(t))
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || res.URL != "https://i.imgur.com/abc123.jpg" {
		t.Fatalf("resultado inesperado: %+v", res)
	}
}

// redirectTransport redireciona toda requisição pro servidor de teste,
// preservando path/query — necessário porque a URL da API é fixa no código.
type redirectTransport struct {
	target string
}

func (rt redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(rt.target + req.URL.Path)
	if err != nil {
		return nil, err
	}
	newReq := req.Clone(req.Context())
	newReq.URL = u
	newReq.Host = u.Host
	return http.DefaultTransport.RoundTrip(newReq)
}
