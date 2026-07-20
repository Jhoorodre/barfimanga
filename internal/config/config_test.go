package config

import (
	"os"
	"strings"
	"testing"
)

// TestSaveConfigCifraHostTokenNoDisco garante o ciclo completo: HostToken em
// texto puro na struct em memória vira cifrado (prefixo enc1:) no
// profiles.json salvo em disco, e volta a texto puro correto ao recarregar —
// sem que o SaveConfig mute o HostToken do MultiConfig que o chamador ainda
// está usando na mesma sessão.
func TestSaveConfigCifraHostTokenNoDisco(t *testing.T) {
	dir := t.TempDir()
	oldwd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(oldwd) })

	const plain = "minha-api-key-em-texto-puro"

	mCfg := MultiConfig{
		ActiveProfile: "default",
		Profiles: map[string]Config{
			"default": {DefaultHost: "imgbb", HostToken: plain, Library: map[string]MangaEntry{}},
		},
	}

	if err := SaveConfig(mCfg); err != nil {
		t.Fatal(err)
	}

	// O struct em memória do chamador não pode ter sido mutado pelo Save.
	if got := mCfg.Profiles["default"].HostToken; got != plain {
		t.Fatalf("SaveConfig mutou o HostToken em memória: %q", got)
	}

	raw, err := os.ReadFile("bd/profiles.json")
	if err != nil {
		t.Fatal(err)
	}
	if content := string(raw); !strings.Contains(content, encPrefix) || strings.Contains(content, plain) {
		t.Fatalf("profiles.json deveria conter o token cifrado (prefixo %q) e nunca o texto puro; conteúdo:\n%s", encPrefix, content)
	}

	reloaded, err := LoadConfig()
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.GetActive().HostToken; got != plain {
		t.Fatalf("token recarregado = %q, esperado %q", got, plain)
	}
}
