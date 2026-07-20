package config

import "testing"

func TestEncryptDecryptTokenRoundtrip(t *testing.T) {
	original := "ghp_abcdefghijklmnopqrstuvwxyz0123456789"

	enc, err := EncryptToken(original)
	if err != nil {
		t.Fatal(err)
	}
	if enc == original {
		t.Fatal("token cifrado não pode ser igual ao original")
	}

	dec, err := DecryptToken(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != original {
		t.Fatalf("token descriptografado = %q, esperado %q", dec, original)
	}
}

func TestEncryptTokenVazioFicaVazio(t *testing.T) {
	enc, err := EncryptToken("")
	if err != nil {
		t.Fatal(err)
	}
	if enc != "" {
		t.Fatalf("token vazio deveria continuar vazio, veio %q", enc)
	}
}

func TestDecryptTokenTextoPuroPassthrough(t *testing.T) {
	// Tokens salvos por versões antigas (sem o prefixo enc1:) devem continuar
	// funcionando sem exigir reconfiguração.
	plain := "minha-api-key-antiga"
	dec, err := DecryptToken(plain)
	if err != nil {
		t.Fatal(err)
	}
	if dec != plain {
		t.Fatalf("esperava passthrough de %q, veio %q", plain, dec)
	}
}

func TestDecryptTokenCifradoCorrompidoFalha(t *testing.T) {
	if _, err := DecryptToken(encPrefix + "isso-nao-e-base64-valido!!!"); err == nil {
		t.Fatal("esperava erro ao descriptografar ciphertext corrompido")
	}
}
