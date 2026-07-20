package config

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"
)

// encPrefix marca texto cifrado salvo no config, para diferenciar de tokens em
// texto puro salvos por versões antigas (compatibilidade retroativa).
const encPrefix = "enc1:"

// deriveKey monta uma chave AES de 32 bytes a partir de identificadores da
// máquina + usuário. Determinístico na mesma máquina/usuário, nunca salvo em
// disco — por isso não pede senha, mas o valor cifrado só abre nessa máquina.
func deriveKey() []byte {
	home, _ := os.UserHomeDir()
	raw := "barfimanga-token-v1\x00" + machineID() + "\x00" + home
	h := sha256.Sum256([]byte(raw))
	return h[:]
}

func machineID() string {
	if runtime.GOOS == "windows" {
		if id := os.Getenv("COMPUTERNAME"); id != "" {
			return id
		}
		h, _ := os.Hostname()
		return h
	}
	for _, path := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if data, err := os.ReadFile(path); err == nil {
			if id := strings.TrimSpace(string(data)); id != "" {
				return id
			}
		}
	}
	h, _ := os.Hostname()
	return h
}

// EncryptToken cifra o token com AES-GCM. String vazia retorna vazia (evita
// gravar um blob cifrado no lugar de "sem token configurado").
func EncryptToken(plaintext string) (string, error) {
	if plaintext == "" {
		return "", nil
	}
	block, err := aes.NewCipher(deriveKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", err
	}
	ct := gcm.Seal(nonce, nonce, []byte(plaintext), nil)
	return encPrefix + base64.StdEncoding.EncodeToString(ct), nil
}

// DecryptToken devolve o token em texto puro. Se "stored" não começar com
// encPrefix, é tratado como já texto puro (tokens salvos antes desta versão).
func DecryptToken(stored string) (string, error) {
	if stored == "" || !strings.HasPrefix(stored, encPrefix) {
		return stored, nil
	}
	data, err := base64.StdEncoding.DecodeString(stored[len(encPrefix):])
	if err != nil {
		return "", fmt.Errorf("base64: %w", err)
	}
	block, err := aes.NewCipher(deriveKey())
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	ns := gcm.NonceSize()
	if len(data) < ns {
		return "", fmt.Errorf("ciphertext muito curto")
	}
	pt, err := gcm.Open(nil, data[:ns], data[ns:], nil)
	if err != nil {
		return "", fmt.Errorf("decrypt: %w", err)
	}
	return string(pt), nil
}
