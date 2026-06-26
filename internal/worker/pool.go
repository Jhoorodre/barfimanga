package worker

import (
	"context"
	"sync"
	"time"

	"barfimanga/internal/cache"
	"barfimanga/internal/hosts"
	"barfimanga/internal/models"
	"barfimanga/internal/progress"
	"golang.org/x/time/rate"
)

// Pool gerencia o upload paralelo de arquivos com controle de taxa de requisições.
type Pool struct {
	host       hosts.Host
	workers    int
	limiter    *rate.Limiter
	maxRetries int
}

// NewPool cria um novo pool. requestsPerSecond define a quantidade de chamadas por segundo.
func NewPool(host hosts.Host, workers int, requestsPerSecond float64, maxRetries int) *Pool {
	var limit rate.Limit
	if requestsPerSecond > 0 {
		limit = rate.Limit(requestsPerSecond)
	} else {
		limit = rate.Inf
	}
	if maxRetries <= 0 {
		maxRetries = 8
	}

	return &Pool{
		host:       host,
		workers:    workers,
		limiter:    rate.NewLimiter(limit, 1),
		maxRetries: maxRetries,
	}
}

type job struct {
	filepath string
	index    int
}

type result struct {
	res   models.UploadResult
	index int
}

// ProcessImages despacha uploads em paralelo mantendo a ordem dos resultados.
func (p *Pool) ProcessImages(ctx context.Context, images []string, tracker *progress.ProgressTracker, uploadCache *cache.UploadCache, forceRebuild bool) ([]models.UploadResult, error) {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	jobs := make(chan job, len(images))
	results := make(chan result, len(images))

	var wg sync.WaitGroup

	// Spawna os workers
	for i := 0; i < p.workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				// Controle de taxa
				if err := p.limiter.Wait(ctx); err != nil {
					// Contexto foi cancelado
					return
				}

				var res models.UploadResult
				var hash string

				// 1. Tentar pegar do cache (Pula se for Rebuild)
				if uploadCache != nil && !forceRebuild {
					h, err := cache.HashFile(j.filepath)
					if err == nil {
						hash = h
						if cachedURL, exists := uploadCache.Get(hash); exists {
							res = models.UploadResult{
								URL:      cachedURL,
								Filename: j.filepath,
								Success:  true,
								Error:    "from_cache",
							}
						}
					}
				}

				// 2. Se não estava no cache (ou falhou hash ou é rebuild), faz o upload
				if res.URL == "" {
					// Se for rebuild mas não calculou hash ainda (pra salvar depois)
					if forceRebuild && uploadCache != nil && hash == "" {
						hash, _ = cache.HashFile(j.filepath)
					}
					var uploadRes models.UploadResult
					var uploadErr error

					// Retry logic agressivo para lidar com falhas de ActiveModel do backend Ruby (ImgBox)
					for attempt := 1; attempt <= p.maxRetries; attempt++ {
						uploadRes, uploadErr = p.host.UploadImage(ctx, j.filepath)
						if uploadErr == nil && uploadRes.Success {
							break
						}

						if attempt < p.maxRetries {
							// Exponential backoff mais conservador: 2s, 4s, 8s, 16s, 32s...
							backoffTime := 1 << attempt
							select {
							case <-time.After(time.Duration(backoffTime) * time.Second):
							case <-ctx.Done():
								return
							}
						}
					}

					if uploadErr != nil || !uploadRes.Success {
						errMsg := "unknown error"
						if uploadErr != nil {
							errMsg = uploadErr.Error()
						} else if uploadRes.Error != "" {
							errMsg = uploadRes.Error
						}

						res = models.UploadResult{
							URL:      "",
							Filename: j.filepath,
							Success:  false,
							Error:    errMsg,
						}
					} else {
						res = uploadRes
						// 3. Salva no cache em caso de sucesso
						if res.Success && uploadCache != nil && hash != "" {
							uploadCache.Set(hash, res.URL)
						}
					}
				}

				results <- result{res: res, index: j.index}

				if tracker != nil {
					tracker.Increment()
				}
			}
		}()
	}

	// Alimenta o canal de jobs com os arquivos a serem enviados
	for i, img := range images {
		jobs <- job{filepath: img, index: i}
	}
	close(jobs)

	// Goroutine auxiliar para aguardar o fim de todos os processos sem bloquear o canal principal
	go func() {
		wg.Wait()
		close(results)
	}()

	// Coleta os resultados mantendo a ordem baseada no index
	finalResults := make([]models.UploadResult, len(images))
	for r := range results {
		finalResults[r.index] = r.res
	}

	return finalResults, nil
}
