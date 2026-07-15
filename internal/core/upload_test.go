package core

import "testing"

func TestChapterKeyAceitaNumeroPrimeiroOuComPrefixo(t *testing.T) {
	cases := []struct {
		folder string
		want   string
	}{
		{"65 - O Poço Oculto e a Luz da Lua (2)", "065"},
		{"66 - O Poço Oculto e a Luz da Lua (3)", "066"},
		{"Cap 019.1 - Título", "019.1"},
		{"Cap 037 - Título", "037"},
		{"Cap 000", "000"},
	}

	seen := make(map[string]string)
	for _, c := range cases {
		got := chapterKey(c.folder)
		if got != c.want {
			t.Errorf("chapterKey(%q) = %q, esperado %q", c.folder, got, c.want)
		}
		if prev, ok := seen[got]; ok {
			t.Errorf("colisão de chave: %q e %q geraram a mesma chave %q", prev, c.folder, got)
		}
		seen[got] = c.folder
	}
}

func TestChapterNumberFromNameAceitaNumeroPrimeiroOuComPrefixo(t *testing.T) {
	cases := []struct {
		folder string
		want   float64
		ok     bool
	}{
		{"65 - O Poço Oculto e a Luz da Lua (2)", 65, true},
		{"Cap 019.1 - Título", 19.1, true},
		{"Extra", 0, false},
	}

	for _, c := range cases {
		got, ok := chapterNumberFromName(c.folder)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("chapterNumberFromName(%q) = (%v, %v), esperado (%v, %v)", c.folder, got, ok, c.want, c.ok)
		}
	}
}
