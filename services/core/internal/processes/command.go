package processes

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

func Executable(names ...string) (string, error) {
	for _, name := range names {
		if path, e := exec.LookPath(name); e == nil {
			return path, nil
		}
	}
	return "", errors.New("runtime não instalado ou não encontrado no PATH")
}

// Resolve package-manager launchers without a shell: Windows .cmd shims are never interpreted.
func Command(ctx context.Context, tool string, args ...string) (*exec.Cmd, error) {
	path, e := exec.LookPath(tool)
	if e != nil {
		return nil, e
	}
	if runtime.GOOS == "windows" && (filepath.Ext(path) == ".cmd" || filepath.Ext(path) == ".bat") {
		node, e := Executable("node")
		if e != nil {
			return nil, e
		}
		var candidates []string
		switch tool {
		case "npm":
			candidates = []string{filepath.Join(filepath.Dir(node), "node_modules", "npm", "bin", "npm-cli.js"), filepath.Join(filepath.Dir(path), "node_modules", "npm", "bin", "npm-cli.js")}
		case "pnpm":
			candidates = []string{filepath.Join(filepath.Dir(path), "node_modules", "pnpm", "bin", "pnpm.cjs"), filepath.Join(filepath.Dir(node), "node_modules", "corepack", "dist", "pnpm.js")}
		case "yarn":
			candidates = []string{filepath.Join(filepath.Dir(path), "node_modules", "yarn", "bin", "yarn.js"), filepath.Join(filepath.Dir(node), "node_modules", "corepack", "dist", "yarn.js")}
		}
		for _, candidate := range candidates {
			if _, e := os.Stat(candidate); e == nil {
				return exec.CommandContext(ctx, node, append([]string{candidate}, args...)...), nil
			}
		}
		return nil, errors.New("launcher .cmd sem entrada JavaScript conhecida; instale o gerenciador pelo Node/Corepack")
	}
	return exec.CommandContext(ctx, path, args...), nil
}
