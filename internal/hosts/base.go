package hosts

import (
	"context"

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
