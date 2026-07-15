package tui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/huh"
)

// Reproduz o bug: colar texto multi-linha no campo Descrição fazia cada
// quebra de linha "vazar" para o próximo campo do formulário, porque o
// keymap padrão do huh trata Enter como avanço de campo em campos de texto.
// Este teste garante que, com o keymap customizado usado em
// editMangaEntry, Enter só insere quebra de linha e nunca dispara avanço.
func TestDescricaoKeyMapEnterNaoAvancaCampo(t *testing.T) {
	var desc string

	km := huh.NewDefaultKeyMap()
	km.Text.Next = key.NewBinding(key.WithKeys("tab"))
	km.Text.Submit = key.NewBinding(key.WithKeys("tab"))
	km.Text.NewLine = key.NewBinding(key.WithKeys("enter", "alt+enter", "ctrl+j"))

	field := huh.NewText().Value(&desc).WithKeyMap(km)
	field.Init()
	field.Focus()

	type_ := func(f huh.Field, s string) huh.Field {
		model, _ := f.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
		return model.(huh.Field)
	}

	field = type_(field, "linha1")

	model, cmd := field.Update(tea.KeyMsg{Type: tea.KeyEnter})
	field = model.(huh.Field)
	if cmd != nil {
		if msg := cmd(); msg != nil {
			typ := strings.ToLower(fmt.Sprintf("%T", msg))
			if strings.Contains(typ, "nextfield") || strings.Contains(typ, "submit") {
				t.Fatalf("Enter não deveria avançar de campo, mas produziu comando do tipo %s", typ)
			}
		}
	}

	field = type_(field, "linha2")

	if !strings.Contains(desc, "linha1\nlinha2") {
		t.Fatalf("esperava quebra de linha preservada no valor, veio %q", desc)
	}

	// Tab continua avançando de campo normalmente.
	model, cmd = field.Update(tea.KeyMsg{Type: tea.KeyTab})
	_ = model
	if cmd == nil {
		t.Fatal("Tab deveria disparar avanço de campo, mas não gerou nenhum comando")
	}
	msg := cmd()
	typ := strings.ToLower(fmt.Sprintf("%T", msg))
	if !strings.Contains(typ, "nextfield") {
		t.Fatalf("Tab deveria produzir o comando de avanço de campo, veio %s", typ)
	}
}
