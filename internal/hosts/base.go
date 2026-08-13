package hosts

import (
	"context"
	"time"

	"barfimanga/internal/models"
)

// Host define a interface que todos os provedores de hospedagem devem implementar.
type Host interface {
	// UploadImage faz o upload de uma única imagem para o servidor.
	UploadImage(ctx context.Context, filepath string) (models.UploadResult, error)

	// CreateAlbum agrupa as imagens enviadas em um álbum.
	// Retorna a URL do álbum ou uma string vazia se o provedor não suportar álbuns.
	CreateAlbum(ctx context.Context, title, description string, imageIDs []string) (string, error)

	// Name retorna o nome de exibição do provedor de hospedagem (ex: "Catbox").
	Name() string
}

// QuotaReporter é implementado opcionalmente por hosts que têm cota de
// upload própria (separada do rate-limit de requisição) e pausam pra
// esperá-la liberar — hoje só o ImgChest. Usado pelo resumo do lote pra
// mostrar quantos desses ciclos ocorreram e quanto tempo foi gasto neles.
type QuotaReporter interface {
	QuotaCycles() (waits int, totalWait time.Duration)
}
