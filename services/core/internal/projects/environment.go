package projects

import (
	"cloudforge/core/internal/model"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var envKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_.-]*$`)

type EnvironmentVariable = model.EnvironmentVariable

func ValidateEnvironment(values []EnvironmentVariable) error {
	if len(values) > 512 {
		return errors.New("limite de 512 variáveis por projeto")
	}
	seen := map[string]bool{}
	for _, v := range values {
		if !envKey.MatchString(v.Key) {
			return fmt.Errorf("nome de variável inválido: %q", v.Key)
		}
		if seen[v.Key] {
			return fmt.Errorf("variável duplicada: %s", v.Key)
		}
		if strings.ContainsAny(v.Value, "\x00\r\n") || len(v.Value) > 8192 {
			return fmt.Errorf("valor inválido para %s", v.Key)
		}
		seen[v.Key] = true
	}
	return nil
}

func EnvironmentFiles(root string) ([]string, error) {
	root, err := Directory(root)
	if err != nil {
		return nil, err
	}
	files := []string{}
	err = filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.IsDir() {
			if rel != "." && (strings.Count(rel, string(os.PathSeparator)) >= 3 || entry.Name() == "node_modules" || entry.Name() == ".git" || entry.Name() == ".venv" || entry.Name() == "venv" || entry.Name() == "dist") {
				return filepath.SkipDir
			}
			return nil
		}
		name := entry.Name()
		if name != ".env" && !strings.HasPrefix(name, ".env.") {
			return nil
		}
		if strings.HasSuffix(name, ".example") || strings.HasSuffix(name, ".sample") || strings.HasSuffix(name, ".template") {
			return nil
		}
		if entry.Type()&os.ModeSymlink != 0 {
			return nil
		}
		if len(files) >= 100 {
			return errors.New("muitos arquivos .env no projeto")
		}
		files = append(files, filepath.ToSlash(rel))
		return nil
	})
	sort.Strings(files)
	return files, err
}

func environmentPath(root, name string) (string, error) {
	files, err := EnvironmentFiles(root)
	if err != nil {
		return "", err
	}
	for _, file := range files {
		if file == name {
			return SafePath(root, name)
		}
	}
	return "", errors.New("arquivo de variáveis não encontrado no projeto")
}

func ReadEnvironment(root, name string) ([]EnvironmentVariable, error) {
	path, err := environmentPath(root, name)
	if err != nil {
		return nil, err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(raw) > 1024*1024 {
		return nil, errors.New("arquivo .env excede 1 MB")
	}
	if strings.ContainsRune(string(raw), 0) {
		return nil, errors.New("arquivo .env binário")
	}
	values := []EnvironmentVariable{}
	for _, line := range strings.Split(string(raw), "\n") {
		key, value, ok := parseEnvironmentLine(line)
		if ok {
			values = append(values, EnvironmentVariable{Key: key, Value: value})
		}
	}
	return values, nil
}

func parseEnvironmentLine(line string) (string, string, bool) {
	line = strings.TrimSpace(line)
	line = strings.TrimPrefix(line, "export ")
	key, value, ok := strings.Cut(line, "=")
	key = strings.TrimSpace(key)
	if !ok || !envKey.MatchString(key) {
		return "", "", false
	}
	value = strings.TrimSpace(value)
	if len(value) >= 2 && (value[0] == '"' || value[0] == '\'') {
		quote := value[0]
		for i := 1; i < len(value); i++ {
			if value[i] == '\\' && quote == '"' && i+1 < len(value) {
				i++
				continue
			}
			if value[i] == quote {
				value = value[1:i]
				if quote == '"' {
					value = strings.ReplaceAll(strings.ReplaceAll(value, `\"`, `"`), `\\`, `\`)
				}
				return key, value, true
			}
		}
	}
	if i := strings.Index(value, " #"); i >= 0 {
		value = strings.TrimSpace(value[:i])
	}
	if strings.HasPrefix(value, "#") {
		value = ""
	}
	return key, value, true
}

func WriteEnvironment(root, name string, values []EnvironmentVariable) error {
	if err := ValidateEnvironment(values); err != nil {
		return err
	}
	path, err := environmentPath(root, name)
	if err != nil {
		return err
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(raw) > 1024*1024 {
		return errors.New("arquivo .env excede 1 MB")
	}
	remaining := map[string]string{}
	for _, v := range values {
		remaining[v.Key] = v.Value
	}
	lines := []string{}
	for _, line := range strings.Split(strings.TrimSuffix(string(raw), "\n"), "\n") {
		key, _, ok := parseEnvironmentLine(line)
		if !ok {
			lines = append(lines, line)
			continue
		}
		if value, exists := remaining[key]; exists {
			lines = append(lines, key+"="+quoteEnvironment(value))
			delete(remaining, key)
		}
	}
	for _, v := range values {
		if value, ok := remaining[v.Key]; ok {
			lines = append(lines, v.Key+"="+quoteEnvironment(value))
		}
	}
	output := strings.Join(lines, "\n") + "\n"
	if len(output) > 1024*1024 {
		return errors.New("arquivo .env excede 1 MB")
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	return os.WriteFile(path, []byte(output), info.Mode().Perm())
}

func quoteEnvironment(value string) string {
	if value == "" || strings.ContainsAny(value, " #\t\"'\\") {
		return `"` + strings.ReplaceAll(strings.ReplaceAll(value, `\`, `\\`), `"`, `\"`) + `"`
	}
	return value
}
