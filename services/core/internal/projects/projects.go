package projects

import (
	"cloudforge/core/internal/auth"
	"cloudforge/core/internal/model"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

type Detection struct {
	Path           string   `json:"path"`
	Runtimes       []string `json:"runtimes"`
	PythonFiles    []string `json:"pythonFiles"`
	Scripts        []string `json:"scripts"`
	PackageManager string   `json:"packageManager"`
	SuggestedName  string   `json:"suggestedName"`
}

func Exists(path string) bool { _, e := os.Stat(path); return e == nil }
func Directory(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("selecione um caminho absoluto")
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", fmt.Errorf("diretório indisponível: %w", e)
	}
	info, e := os.Stat(resolved)
	if e != nil || !info.IsDir() {
		return "", errors.New("caminho não é um diretório")
	}
	return filepath.Clean(resolved), nil
}
func Detect(path string) (Detection, error) {
	root, e := Directory(path)
	d := Detection{Runtimes: []string{}, PythonFiles: []string{}, Scripts: []string{}}
	if e != nil {
		return d, e
	}
	d.Path = root
	d.SuggestedName = filepath.Base(root)
	entries, e := os.ReadDir(root)
	if e != nil {
		return d, e
	}
	for _, f := range entries {
		if !f.IsDir() && strings.HasSuffix(strings.ToLower(f.Name()), ".py") {
			d.PythonFiles = append(d.PythonFiles, f.Name())
		}
	}
	node := Exists(filepath.Join(root, "package.json"))
	pythonManifest := Exists(filepath.Join(root, "requirements.txt")) || Exists(filepath.Join(root, "pyproject.toml")) || Exists(filepath.Join(root, "Pipfile"))
	if node {
		d.Runtimes = append(d.Runtimes, "node")
		var pkg struct {
			Name           string            `json:"name"`
			Scripts        map[string]string `json:"scripts"`
			PackageManager string            `json:"packageManager"`
		}
		raw, e := os.ReadFile(filepath.Join(root, "package.json"))
		if e != nil {
			return d, e
		}
		if e = json.Unmarshal(raw, &pkg); e != nil {
			return d, errors.New("package.json inválido")
		}
		for script := range pkg.Scripts {
			d.Scripts = append(d.Scripts, script)
		}
		sort.Strings(d.Scripts)
		declared := strings.Split(pkg.PackageManager, "@")[0]
		if declared != "" && declared != "npm" && declared != "pnpm" && declared != "yarn" {
			return d, errors.New("packageManager não suportado; use npm, pnpm ou yarn")
		}
		locks := []string{}
		for _, pair := range [][2]string{{"npm", "package-lock.json"}, {"pnpm", "pnpm-lock.yaml"}, {"yarn", "yarn.lock"}} {
			if Exists(filepath.Join(root, pair[1])) {
				locks = append(locks, pair[0])
			}
		}
		if len(locks) > 1 && declared == "" {
			return d, errors.New("há múltiplos lockfiles; declare packageManager no package.json ou mantenha somente o lockfile utilizado")
		}
		d.PackageManager = "npm"
		if len(locks) == 1 {
			d.PackageManager = locks[0]
		}
		if declared != "" {
			d.PackageManager = declared
		}
	}
	if pythonManifest || (!node && len(d.PythonFiles) > 0) {
		d.Runtimes = append(d.Runtimes, "python")
	}
	return d, nil
}

var safeName = regexp.MustCompile(`^[a-zA-Z0-9_.:@/-]+$`)

func Validate(p *model.Project) error {
	p.Name = strings.TrimSpace(p.Name)
	if len(p.Name) < 1 || len(p.Name) > 100 {
		return errors.New("nome deve ter entre 1 e 100 caracteres")
	}
	root, e := Directory(p.Path)
	if e != nil {
		return e
	}
	p.Path = root
	if p.WorkingDirectory == "" {
		p.WorkingDirectory = root
	}
	wd, e := SafePath(root, p.WorkingDirectory)
	if e != nil {
		return errors.New("diretório de trabalho deve estar dentro do projeto")
	}
	info, e := os.Stat(wd)
	if e != nil || !info.IsDir() {
		return errors.New("diretório de trabalho inválido")
	}
	p.WorkingDirectory = wd
	if p.RestartPolicy == "" {
		p.RestartPolicy = "on-failure"
	}
	if p.RestartPolicy != "never" && p.RestartPolicy != "on-failure" && p.RestartPolicy != "always" {
		return errors.New("política de reinicialização inválida")
	}
	if p.Port < 0 || p.Port > 65535 {
		return errors.New("porta inválida")
	}
	return ValidateStartup(p)
}

func ValidateStartup(p *model.Project) error {
	if len(p.Arguments) > 64 {
		return errors.New("argumentos demais")
	}
	root := p.Path
	switch p.Runtime {
	case "python":
		entry, e := SafePath(root, p.EntryPoint)
		if e != nil {
			return e
		}
		if !strings.HasSuffix(strings.ToLower(entry), ".py") {
			return errors.New("selecione um arquivo Python")
		}
		info, e := os.Stat(entry)
		if e != nil || info.IsDir() {
			return errors.New("arquivo principal inexistente")
		}
		p.EntryPoint = entry
	case "node":
		if len(p.StartCommand) < 2 || len(p.StartCommand) > 64 {
			return errors.New("configure um comando Node.js válido")
		}
		tool := p.StartCommand[0]
		if tool != "node" && tool != "npm" && tool != "pnpm" && tool != "yarn" {
			return errors.New("use node, npm, pnpm ou yarn; comandos de shell não são permitidos")
		}
		if tool == "node" {
			entry, e := SafePath(root, p.StartCommand[1])
			if e != nil {
				return e
			}
			if !Exists(entry) {
				return errors.New("arquivo JavaScript inexistente")
			}
		} else {
			if len(p.StartCommand) != 3 || p.StartCommand[1] != "run" || !safeName.MatchString(p.StartCommand[2]) {
				return errors.New("use gerenciador run nome-do-script")
			}
			d, e := Detect(root)
			if e != nil {
				return e
			}
			found := false
			for _, s := range d.Scripts {
				if s == p.StartCommand[2] {
					found = true
				}
			}
			if !found {
				return errors.New("script não existe no package.json")
			}
		}
	default:
		return errors.New("runtime deve ser python ou node")
	}
	return nil
}
func New(p *model.Project) error {
	if e := Validate(p); e != nil {
		return e
	}
	p.ID = auth.ID()
	p.DesiredState = "stopped"
	p.CreatedAt = time.Now().UTC()
	p.UpdatedAt = p.CreatedAt
	return nil
}
func SafePath(root, path string) (string, error) {
	if path == "" {
		path = "."
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	resolved, e := filepath.EvalSymlinks(path)
	if e != nil {
		return "", e
	}
	rel, e := filepath.Rel(root, resolved)
	if e != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) || filepath.IsAbs(rel) {
		return "", errors.New("acesso fora do diretório do projeto bloqueado")
	}
	return resolved, nil
}

type File struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	Directory bool      `json:"directory"`
	Size      int64     `json:"size"`
	Modified  time.Time `json:"modified"`
}

func Files(root, path string) ([]File, error) {
	dir, e := SafePath(root, path)
	if e != nil {
		return nil, e
	}
	entries, e := os.ReadDir(dir)
	if e != nil {
		return nil, e
	}
	out := []File{}
	for _, entry := range entries {
		full := filepath.Join(dir, entry.Name())
		safe, e := SafePath(root, full)
		if e != nil {
			continue
		}
		info, e := os.Stat(safe)
		if e != nil {
			continue
		}
		rel, _ := filepath.Rel(root, full)
		out = append(out, File{entry.Name(), rel, info.IsDir(), info.Size(), info.ModTime()})
		if len(out) >= 5000 {
			break
		}
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Directory != out[j].Directory {
			return out[i].Directory
		}
		return out[i].Name < out[j].Name
	})
	return out, nil
}
func Read(root, path string) (string, error) {
	file, e := SafePath(root, path)
	if e != nil {
		return "", e
	}
	info, e := os.Stat(file)
	if e != nil {
		return "", e
	}
	if info.Size() > 5*1024*1024 {
		return "", errors.New("visualização limitada a arquivos de até 5 MB")
	}
	data, e := os.ReadFile(file)
	if e != nil {
		return "", e
	}
	if strings.ContainsRune(string(data), 0) {
		return "", errors.New("arquivo binário; abra pelo explorador do sistema")
	}
	return string(data), nil
}

// ReadText includes a version derived from the file bytes so edits can detect
// changes made outside Cloudforge while the editor is open.
func ReadText(root, path string) (string, string, error) {
	content, err := Read(root, path)
	if err != nil {
		return "", "", err
	}
	if !utf8.ValidString(content) {
		return "", "", errors.New("arquivo de texto não está em UTF-8")
	}
	hash := sha256.Sum256([]byte(content))
	return content, hex.EncodeToString(hash[:]), nil
}

func WriteText(root, path, content, expectedVersion string) (string, error) {
	if path == "" || expectedVersion == "" {
		return "", errors.New("caminho e versão do arquivo são obrigatórios")
	}
	if len(content) > 5*1024*1024 || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return "", errors.New("salvamento limitado a texto UTF-8 de até 5 MB")
	}
	file, err := SafePath(root, path)
	if err != nil {
		return "", err
	}
	info, err := os.Stat(file)
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() || info.Size() > 5*1024*1024 {
		return "", errors.New("somente arquivos de texto de até 5 MB podem ser editados")
	}
	current, err := os.ReadFile(file)
	if err != nil {
		return "", err
	}
	hash := sha256.Sum256(current)
	if hex.EncodeToString(hash[:]) != expectedVersion {
		return "", errors.New("o arquivo mudou no disco; reabra-o antes de salvar")
	}
	if !utf8.Valid(current) || strings.ContainsRune(string(current), 0) {
		return "", errors.New("arquivo binário não pode ser editado como texto")
	}
	if err := os.WriteFile(file, []byte(content), info.Mode().Perm()); err != nil {
		return "", err
	}
	hash = sha256.Sum256([]byte(content))
	return hex.EncodeToString(hash[:]), nil
}
