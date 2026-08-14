package worker_test

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"barfimanga/internal/models"
	"barfimanga/internal/progress"
	"barfimanga/internal/worker"
)

// mockHost is a dummy implementation of hosts.Host for testing purposes
type mockHost struct {
	shouldFail bool
}

func (m *mockHost) Name() string {
	return "MockHost"
}

func (m *mockHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}

func (m *mockHost) UploadImage(ctx context.Context, filepath string) (models.UploadResult, error) {
	time.Sleep(10 * time.Millisecond) // Simulate network delay

	if m.shouldFail && filepath == "fail.jpg" {
		return models.UploadResult{}, fmt.Errorf("simulated upload error")
	}

	return models.UploadResult{
		URL:      "https://mockhost.com/" + filepath,
		Filename: filepath,
		Success:  true,
		Error:    "",
	}, nil
}

func TestPool_ProcessImages_Success(t *testing.T) {
	host := &mockHost{shouldFail: false}
	pool := worker.NewPool(host, 2, 0, 1) // No rate limit, 2 workers

	images := []string{"img1.jpg", "img2.jpg", "img3.jpg"}
	tracker := &progress.ProgressTracker{Total: int64(len(images))}

	results, err := pool.ProcessImages(context.Background(), images, tracker, nil, false)

	if err != nil {
		t.Fatalf("Expected no error, got: %v", err)
	}

	if len(results) != 3 {
		t.Fatalf("Expected 3 results, got: %d", len(results))
	}

	for i, res := range results {
		if !res.Success {
			t.Errorf("Expected image %d to succeed, but it failed", i)
		}
		expectedURL := "https://mockhost.com/img" + fmt.Sprintf("%d", i+1) + ".jpg"
		if res.URL != expectedURL {
			t.Errorf("Expected URL %s, got: %s", expectedURL, res.URL)
		}
	}

	if tracker.Done.Load() != 3 {
		t.Errorf("Expected tracker to reach 3, got: %d", tracker.Done.Load())
	}
}

func TestPool_ProcessImages_Failure(t *testing.T) {
	host := &mockHost{shouldFail: true}
	pool := worker.NewPool(host, 1, 0, 1)

	images := []string{"ok1.jpg", "fail.jpg", "ok2.jpg"}

	results, err := pool.ProcessImages(context.Background(), images, nil, nil, false)

	if err != nil {
		t.Fatalf("Expected pool to complete despite individual image failures, got: %v", err)
	}

	if results[0].Success != true {
		t.Errorf("Expected ok1 to succeed")
	}

	if results[1].Success != false {
		t.Errorf("Expected fail.jpg to fail")
	}

	if results[2].Success != true {
		t.Errorf("Expected ok2 to succeed")
	}
}

func TestPool_RateLimiting(t *testing.T) {
	host := &mockHost{shouldFail: false}
	// Limit to 10 requests per second (100ms per request)
	pool := worker.NewPool(host, 5, 10.0, 1)

	images := []string{"1", "2", "3"}

	start := time.Now()
	_, _ = pool.ProcessImages(context.Background(), images, nil, nil, false)
	elapsed := time.Since(start)

	// With 10 req/sec, processing 3 images should take at least ~200ms
	// (0ms for first, 100ms for second, 100ms for third).
	// We check if it took more than 150ms to account for test speed variations.
	if elapsed < 150*time.Millisecond {
		t.Errorf("Rate limiter didn't throttle. Execution was too fast: %v", elapsed)
	}
}

// webpRejectingHost simula um host (ex: ImgChest) que rejeita .webp por
// formato mas aceita .jpg — usado pra confirmar que o fallback de
// internal/imgfix (genérico, via worker.Pool) resolve isso pra qualquer
// host, sem cada host precisar implementar a conversão por conta própria.
type webpRejectingHost struct{ acceptedPaths []string }

func (h *webpRejectingHost) Name() string { return "WebpRejectingHost" }

func (h *webpRejectingHost) CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error) {
	return "", nil
}

func (h *webpRejectingHost) UploadImage(ctx context.Context, path string) (models.UploadResult, error) {
	h.acceptedPaths = append(h.acceptedPaths, path)
	if strings.EqualFold(filepath.Ext(path), ".webp") {
		return models.UploadResult{Success: false, Error: "tipo de arquivo invalido"}, fmt.Errorf("erro http 400: tipo de arquivo invalido")
	}
	return models.UploadResult{
		URL:      "https://fake.test/" + filepath.Base(path),
		Filename: filepath.Base(path),
		Success:  true,
	}, nil
}

// TestPool_FallbackWebpParaJPEGQuandoHostRejeita reproduz o caso real do
// ImgChest rejeitando um .webp estendido (VP8X) válido: o Pool deve cair
// pro fallback de JPEG (internal/imgfix) e completar o upload com sucesso,
// preservando o nome de arquivo original no resultado.
func TestPool_FallbackWebpParaJPEGQuandoHostRejeita(t *testing.T) {
	host := &webpRejectingHost{}
	pool := worker.NewPool(host, 1, 0, 3) // até 3 tentativas, dá tempo do fallback entrar

	images := []string{"../imgfix/testdata/extended-with-alpha.webp"}
	results, err := pool.ProcessImages(context.Background(), images, nil, nil, false)
	if err != nil {
		t.Fatalf("ProcessImages retornou erro: %v", err)
	}

	if !results[0].Success {
		t.Fatalf("esperava sucesso via fallback JPEG, veio: %+v", results[0])
	}
	if results[0].Filename != "extended-with-alpha.webp" {
		t.Errorf("esperava que o Filename final preservasse o nome original .webp, veio %q", results[0].Filename)
	}

	sawWebp, sawJpeg := false, false
	for _, p := range host.acceptedPaths {
		switch strings.ToLower(filepath.Ext(p)) {
		case ".webp":
			sawWebp = true
		case ".jpg":
			sawJpeg = true
		}
	}
	if !sawWebp || !sawJpeg {
		t.Errorf("esperava que o host visse uma tentativa .webp (rejeitada) e uma .jpg (aceita), paths=%v", host.acceptedPaths)
	}
}
