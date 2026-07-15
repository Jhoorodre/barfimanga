package utils

import (
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// RemoveAccents remove acentos e marcas de uma string.
func RemoveAccents(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	result, _, _ := transform.String(t, s)
	return result
}

// SanitizeFilename limpa strings para serem usadas com segurança como arquivos ou pastas.
func SanitizeFilename(name string, isFile bool) string {
	if name == "" {
		if isFile {
			return "sem_titulo"
		}
		return "pasta_sem_nome"
	}

	temp := RemoveAccents(name)

	if isFile {
		temp = strings.ReplaceAll(temp, " ", "_")
	}

	re := regexp.MustCompile(`[\\/*?:"<>|]`)
	temp = re.ReplaceAllString(temp, "")

	if isFile {
		ext := filepath.Ext(temp)
		base := strings.TrimSuffix(temp, ext)

		reAlpha := regexp.MustCompile(`[^\w_-]`)
		base = reAlpha.ReplaceAllString(base, "")

		reUnder := regexp.MustCompile(`_+`)
		base = reUnder.ReplaceAllString(base, "_")

		base = strings.Trim(base, "_-")
		if base == "" {
			base = "arquivo_sem_nome"
		}
		temp = base + ext
	} else {
		reFolder := regexp.MustCompile(`[^\w\s_-]`)
		temp = reFolder.ReplaceAllString(temp, "")

		reSpace := regexp.MustCompile(`\s+`)
		temp = reSpace.ReplaceAllString(temp, " ")

		temp = strings.TrimSpace(temp)
	}

	if temp == "" {
		if isFile {
			return "sem_titulo"
		}
		return "pasta_sem_nome"
	}
	return temp
}

// ToWSLPath converte um caminho Windows (ex: D:\...) para o formato WSL (/mnt/d/...)
// Se o usuário passar caminho com aspas ou caminho de rede (\\wsl.localhost\...), ele limpa.
// Só faz sentido para o binário Linux/WSL: no .exe nativo do Windows, "D:\..." já é o
// caminho certo e "/mnt/d/..." não existe, então aqui só limpa aspas e retorna como está.
func ToWSLPath(path string) string {
	// 1. Remove aspas geradas pelo Drag and Drop do Windows
	path = strings.Trim(path, "\"")
	path = strings.Trim(path, "'")

	if runtime.GOOS == "windows" {
		return path
	}

	// 2. Resolve caminhos de rede do WSL (ex: \\wsl.localhost\Ubuntu-22.04\home\...)
	if strings.HasPrefix(path, `\\wsl.localhost\`) || strings.HasPrefix(path, `\\wsl$\`) {
		path = strings.ReplaceAll(path, "\\", "/")
		// "//wsl.localhost/Ubuntu-22.04/home/..." -> dividido pela barra
		parts := strings.SplitN(path, "/", 5)
		if len(parts) >= 5 {
			return "/" + parts[4] // Extrai apenas a parte real do Linux (ex: /home/...)
		}
	}

	// 3. Verifica se parece um caminho Windows tradicional (ex: C:\ ou D:\)
	if len(path) >= 3 && path[1] == ':' && (path[2] == '\\' || path[2] == '/') {
		drive := strings.ToLower(string(path[0]))
		remaining := strings.ReplaceAll(path[3:], "\\", "/")
		return "/mnt/" + drive + "/" + remaining
	}

	// Se já for linux nativo (ou outra coisa maluca), retorna como está
	return path
}

// extractNumbers é usado pela NaturalSort para dividir string e números.
func extractNumbers(s string) []string {
	re := regexp.MustCompile(`\d+|\D+`)
	return re.FindAllString(s, -1)
}

// MoveFile move um arquivo de origem para o destino. Funciona inclusive entre diferentes partições no Linux (EXDEV).
func MoveFile(src, dst string) error {
	err := os.Rename(src, dst)
	if err == nil {
		return nil
	}

	// Se der erro (ex: invalid cross-device link), faz a cópia manual
	srcFile, err := os.Open(src)
	if err != nil {
		return err
	}
	defer srcFile.Close()

	dstFile, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	if _, err = io.Copy(dstFile, srcFile); err != nil {
		return err
	}

	srcFile.Close() // Fecha logo para poder remover
	return os.Remove(src)
}

// NaturalSort ordena uma lista de strings mantendo a progressão numérica correta (ex: 1 antes de 2).
func NaturalSort(files []string) {
	sort.Slice(files, func(i, j int) bool {
		a := extractNumbers(files[i])
		b := extractNumbers(files[j])

		for k := 0; k < len(a) && k < len(b); k++ {
			if a[k] != b[k] {
				numA, errA := strconv.Atoi(a[k])
				numB, errB := strconv.Atoi(b[k])

				if errA == nil && errB == nil {
					return numA < numB
				}
				return strings.ToLower(a[k]) < strings.ToLower(b[k])
			}
		}
		return len(a) < len(b)
	})
}
