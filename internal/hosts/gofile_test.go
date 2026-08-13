package hosts

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"barfimanga/internal/config"
)

// TestGofileUploadAnonimoUsaDownloadPage reproduz o comportamento real
// confirmado ao vivo: sem HostToken (conta gratuita/anônima), a URL final é
// a página de download — não existe link direto sem conta Premium.
func TestGofileUploadAnonimoUsaDownloadPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/servers"):
			w.Write([]byte(`{"status":"ok","data":{"servers":[{"name":"store-eu-par-1","zone":"eu"}]}}`))
		case strings.Contains(r.URL.Path, "/contents/uploadfile"):
			w.Write([]byte(`{"status":"ok","data":{"id":"8d7a1517","downloadPage":"https://gofile.io/d/Y6kmC7jF","guestToken":"abc"}}`))
		default:
			t.Fatalf("path inesperado: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	h := NewGofileHost(config.Config{})
	h.client = &http.Client{Transport: redirectTransport{target: server.URL}}

	res, err := h.UploadImage(context.Background(), writeTempImage(t))
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != "https://gofile.io/d/Y6kmC7jF" {
		t.Fatalf("esperava a downloadPage como URL final (sem token = sem link direto), veio %q", res.URL)
	}
}

// TestGofileUploadComTokenTentaLinkDireto confirma que, com HostToken
// configurado, o host tenta o endpoint de link direto e usa o resultado se
// disponível.
func TestGofileUploadComTokenTentaLinkDireto(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/servers"):
			w.Write([]byte(`{"status":"ok","data":{"servers":[{"name":"store-eu-par-1","zone":"eu"}]}}`))
		case strings.Contains(r.URL.Path, "/contents/uploadfile"):
			if auth := r.Header.Get("Authorization"); auth != "Bearer premium-token" {
				t.Errorf("Authorization no upload = %q", auth)
			}
			w.Write([]byte(`{"status":"ok","data":{"id":"8d7a1517","downloadPage":"https://gofile.io/d/Y6kmC7jF"}}`))
		case strings.Contains(r.URL.Path, "/contents/8d7a1517"):
			if auth := r.Header.Get("Authorization"); auth != "Bearer premium-token" {
				t.Errorf("Authorization no getDirectLink = %q", auth)
			}
			w.Write([]byte(`{"status":"ok","data":{"type":"file","directLink":"https://store-eu-par-1.gofile.io/download/8d7a1517/pagina.jpg"}}`))
		default:
			t.Fatalf("path inesperado: %s", r.URL.Path)
		}
	}))
	defer server.Close()

	h := NewGofileHost(config.Config{HostToken: "premium-token"})
	h.client = &http.Client{Transport: redirectTransport{target: server.URL}}

	res, err := h.UploadImage(context.Background(), writeTempImage(t))
	if err != nil {
		t.Fatal(err)
	}
	if res.URL != "https://store-eu-par-1.gofile.io/download/8d7a1517/pagina.jpg" {
		t.Fatalf("esperava o link direto (com token), veio %q", res.URL)
	}
}
