package databases

import (
	"cloudforge/core/internal/processes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

type dockerStatus string

const (
	dockerRunning      dockerStatus = "running"
	dockerStarting     dockerStatus = "starting"
	dockerMissing      dockerStatus = "missing"
	dockerUnavailable  dockerStatus = "unavailable"
	dockerIncompatible dockerStatus = "incompatible"
)

func dockerExecutable() (string, error) {
	path, err := exec.LookPath("docker")
	if err == nil || runtime.GOOS != "windows" {
		return path, err
	}
	// An app already open when Docker Desktop was installed may have an old PATH.
	for _, dir := range dockerDesktopDirectories() {
		path := filepath.Join(dir, "resources", "bin", "docker.exe")
		if info, err := os.Stat(path); err == nil && !info.IsDir() {
			return path, nil
		}
	}
	return "", err
}

func dockerDesktopDirectories() []string {
	var directories []string
	if dir := os.Getenv("LOCALAPPDATA"); filepath.IsAbs(dir) {
		directories = append(directories, filepath.Join(dir, "Programs", "DockerDesktop"))
	}
	if dir := os.Getenv("ProgramFiles"); filepath.IsAbs(dir) {
		directories = append(directories, filepath.Join(dir, "Docker", "Docker"))
	}
	return directories
}

func runDockerStartup(ctx context.Context, executable string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, executable, args...)
	processes.ConfigureBackground(cmd)
	return cmd.Run()
}

func startLocalDocker(ctx context.Context) bool {
	switch runtime.GOOS {
	case "windows":
		if cli, err := dockerExecutable(); err == nil {
			if runDockerStartup(ctx, cli, "desktop", "start", "--detach") == nil {
				return true
			}
		}
		// Older Docker Desktop installations do not include the Desktop CLI.
		for _, dir := range dockerDesktopDirectories() {
			if ctx.Err() != nil {
				return false
			}
			cmd := exec.Command(filepath.Join(dir, "Docker Desktop.exe"))
			processes.ConfigureBackground(cmd)
			if cmd.Start() == nil {
				go func() { _ = cmd.Wait() }()
				return true
			}
		}
	case "linux":
		if runDockerStartup(ctx, "systemctl", "--user", "--no-ask-password", "start", "docker.service") == nil {
			return true
		}
		return runDockerStartup(ctx, "systemctl", "--no-ask-password", "start", "docker.service") == nil
	}
	return false
}
