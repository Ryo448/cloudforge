package runtimes

import (
	"cloudforge/core/internal/model"
	"cloudforge/core/internal/persistence"
	"cloudforge/core/internal/processes"
	"cloudforge/core/internal/projects"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

type Logger func(string, string)

func fingerprint(path string) string {
	data, e := os.ReadFile(path)
	if e != nil {
		return ""
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}
func version(ctx context.Context, tool string) (string, error) {
	cmd, e := processes.Command(ctx, tool, "--version")
	if e != nil {
		return "", e
	}
	processes.Configure(cmd)
	out, e := cmd.CombinedOutput()
	if e != nil {
		return "", e
	}
	return strings.TrimSpace(string(out)), nil
}

type logWriter struct {
	log    Logger
	stream string
}

func (w logWriter) Write(p []byte) (int, error) {
	for _, line := range strings.Split(strings.TrimSuffix(string(p), "\n"), "\n") {
		w.log(w.stream, line)
	}
	return len(p), nil
}
func run(ctx context.Context, p model.Project, log Logger, tool string, args ...string) error {
	cmd, e := processes.Command(ctx, tool, args...)
	if e != nil {
		return e
	}
	cmd.Dir = p.WorkingDirectory
	cmd.WaitDelay = 2 * time.Second
	cmd.Env = append(os.Environ(), "PIP_DISABLE_PIP_VERSION_CHECK=1", "npm_config_audit=false", "npm_config_fund=false")
	cmd.Stdout = logWriter{log, "stdout"}
	cmd.Stderr = logWriter{log, "stderr"}
	processes.Configure(cmd)
	log("system", "Preparando ambiente: "+filepath.Base(tool)+" "+strings.Join(args, " "))
	if e = cmd.Start(); e != nil {
		return e
	}
	tree, e := processes.Attach(cmd)
	if e != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return e
	}
	defer tree.Close()
	return cmd.Wait()
}
func Prepare(ctx context.Context, s persistence.Repository, p model.Project, log Logger) (*exec.Cmd, error) {
	if e := projects.Validate(&p); e != nil {
		return nil, e
	}
	if p.Runtime == "python" {
		return python(ctx, s, p, log)
	}
	return node(ctx, s, p, log)
}
func python(ctx context.Context, s persistence.Repository, p model.Project, log Logger) (*exec.Cmd, error) {
	names := []string{"python3", "python"}
	if runtime.GOOS == "windows" {
		names = []string{"python", "python3"}
	}
	base, e := processes.Executable(names...)
	if e != nil {
		return nil, e
	}
	baseVersion, e := version(ctx, base)
	if e != nil {
		return nil, e
	}
	venv := ""
	interpreter := ""
	for _, name := range []string{".venv", "venv", "env"} {
		dir := filepath.Join(p.Path, name)
		if !projects.Exists(filepath.Join(dir, "pyvenv.cfg")) {
			continue
		}
		safe, e := projects.SafePath(p.Path, dir)
		if e != nil {
			return nil, errors.New("ambiente virtual aponta para fora do projeto")
		}
		venv = safe
		break
	}
	fresh := false
	if venv == "" {
		venv = filepath.Join(p.Path, ".venv")
		if projects.Exists(venv) {
			return nil, errors.New(".venv existe sem pyvenv.cfg; repare ou remova manualmente antes de continuar")
		}
		if e = run(ctx, p, log, base, "-m", "venv", venv); e != nil {
			return nil, fmt.Errorf("criação da venv: %w", e)
		}
		fresh = true
	}
	if runtime.GOOS == "windows" {
		interpreter = filepath.Join(venv, "Scripts", "python.exe")
	} else {
		interpreter = filepath.Join(venv, "bin", "python")
	}
	v, e := version(ctx, interpreter)
	if e != nil {
		return nil, errors.New("ambiente virtual inválido; recrie a venv pelo Python do projeto")
	}
	probe, e := processes.Command(ctx, interpreter, "-c", "import sys; print(sys.prefix)")
	if e != nil {
		return nil, e
	}
	processes.Configure(probe)
	out, e := probe.CombinedOutput()
	if e != nil || !strings.EqualFold(filepath.Clean(strings.TrimSpace(string(out))), filepath.Clean(venv)) {
		return nil, errors.New("interpretador não pertence à venv detectada")
	}
	hash := fingerprint(filepath.Join(p.Path, "requirements.txt"))
	previous, _ := s.Dependencies(p.ID)
	signature := v + "|base=" + baseVersion + "|venv=" + venv
	environment := environmentStamp(filepath.Join(venv, "pyvenv.cfg")) + "|" + environmentStamp(interpreter)
	if hash != "" && (fresh || previous.ManifestHash != hash || previous.RuntimeVersion != signature || previous.EnvironmentFingerprint != environment || previous.InstallStatus != "success") {
		if e = beginInstall(s, p.ID, &previous); e != nil {
			return nil, e
		}
		if e = run(ctx, p, log, interpreter, "-m", "pip", "install", "-r", filepath.Join(p.Path, "requirements.txt")); e != nil {
			failedInstall(s, previous)
			return nil, fmt.Errorf("instalação Python falhou: %w", e)
		}
		if e = s.SaveDependencies(model.Dependencies{ProjectID: p.ID, ManifestHash: hash, RuntimeVersion: signature, EnvironmentFingerprint: environment, PackageManager: "pip", LastSuccessfulInstall: time.Now().UTC(), InstallStatus: "success"}); e != nil {
			return nil, e
		}
	} else if hash == "" {
		log("system", "Nenhum requirements.txt: instalação automática não configurada.")
	} else {
		log("system", "Dependências Python reutilizadas; requirements e interpretador não mudaram.")
	}
	cmd, e := processes.Command(ctx, interpreter, append([]string{"-u", p.EntryPoint}, p.Arguments...)...)
	return cmd, e
}
func node(ctx context.Context, s persistence.Repository, p model.Project, log Logger) (*exec.Cmd, error) {
	nodeVersion, e := version(ctx, "node")
	if e != nil {
		return nil, e
	}
	d, e := projects.Detect(p.Path)
	if e != nil {
		return nil, e
	}
	manager := d.PackageManager
	if manager == "" {
		return nil, errors.New("projeto Node.js precisa de package.json")
	}
	managerVersion, e := version(ctx, manager)
	if e != nil {
		return nil, fmt.Errorf("gerenciador %s indisponível: %w", manager, e)
	}
	lock := "package-lock.json"
	if manager == "pnpm" {
		lock = "pnpm-lock.yaml"
	}
	if manager == "yarn" {
		lock = "yarn.lock"
	}
	manifestHash := fingerprint(filepath.Join(p.Path, "package.json"))
	lockHash := fingerprint(filepath.Join(p.Path, lock))
	previous, _ := s.Dependencies(p.ID)
	sig := nodeVersion + "|" + managerVersion
	modules := nodeModulesValid(p.Path)
	yarnPnP := manager == "yarn" && projects.Exists(filepath.Join(p.Path, ".pnp.cjs"))
	environment := nodeEnvironment(p.Path, manager)
	if previous.ManifestHash != manifestHash || previous.LockfileHash != lockHash || previous.RuntimeVersion != sig || previous.PackageManager != manager || (!modules && !yarnPnP) || previous.EnvironmentFingerprint != environment || previous.InstallStatus != "success" {
		if e = beginInstall(s, p.ID, &previous); e != nil {
			return nil, e
		}
		args := []string{"install"}
		if manager == "npm" && lockHash != "" {
			args = []string{"ci"}
		}
		if manager == "pnpm" && lockHash != "" {
			args = append(args, "--frozen-lockfile")
		}
		if manager == "yarn" && lockHash != "" {
			if strings.HasPrefix(managerVersion, "1.") {
				args = append(args, "--frozen-lockfile")
			} else {
				args = append(args, "--immutable")
			}
		}
		installProject := p
		installProject.WorkingDirectory = p.Path
		if e = run(ctx, installProject, log, manager, args...); e != nil {
			failedInstall(s, previous)
			return nil, fmt.Errorf("instalação Node.js falhou: %w", e)
		}
		if e = s.SaveDependencies(model.Dependencies{ProjectID: p.ID, ManifestHash: fingerprint(filepath.Join(p.Path, "package.json")), LockfileHash: fingerprint(filepath.Join(p.Path, lock)), RuntimeVersion: sig, EnvironmentFingerprint: nodeEnvironment(p.Path, manager), PackageManager: manager, LastSuccessfulInstall: time.Now().UTC(), InstallStatus: "success"}); e != nil {
			return nil, e
		}
	} else {
		log("system", "Dependências Node.js reutilizadas; manifesto, lockfile e runtime não mudaram.")
	}
	return processes.Command(ctx, p.StartCommand[0], p.StartCommand[1:]...)
}
func environmentStamp(path string) string {
	info, e := os.Stat(path)
	if e != nil {
		return "missing"
	}
	return fmt.Sprintf("%s:%d:%d", fingerprint(path), info.Size(), info.ModTime().UnixNano())
}
func beginInstall(s persistence.Repository, id string, previous *model.Dependencies) error {
	previous.ProjectID = id
	previous.InstallStatus = "installing"
	return s.SaveDependencies(*previous)
}
func failedInstall(s persistence.Repository, previous model.Dependencies) {
	previous.InstallStatus = "failed"
	_ = s.SaveDependencies(previous)
}
func nodeEnvironment(root, manager string) string {
	marker := "node_modules/.package-lock.json"
	if manager == "pnpm" {
		marker = "node_modules/.modules.yaml"
	}
	if manager == "yarn" {
		if projects.Exists(filepath.Join(root, ".pnp.cjs")) {
			marker = ".pnp.cjs"
		} else {
			marker = "node_modules/.yarn-integrity"
		}
	}
	return environmentStamp(filepath.Join(root, marker))
}
func nodeModulesValid(root string) bool {
	var pkg struct {
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	raw, e := os.ReadFile(filepath.Join(root, "package.json"))
	if e != nil || json.Unmarshal(raw, &pkg) != nil {
		return false
	}
	for _, deps := range []map[string]string{pkg.Dependencies, pkg.DevDependencies} {
		for name := range deps {
			if strings.Contains(name, "..") || !projects.Exists(filepath.Join(root, "node_modules", filepath.FromSlash(name), "package.json")) {
				return false
			}
		}
	}
	return true
}
