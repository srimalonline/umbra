package core

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// RealExecutor shells out to docker / docker compose via os/exec. It never
// returns an error for a non-zero exit — the exit code is returned so the core
// decides what a failure means. err is set only when the process fails to start
// or the context deadline fires.
type RealExecutor struct{}

func (RealExecutor) Run(ctx context.Context, name string, args ...string) (string, string, int, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	var stdout, stderr []byte
	stdoutBuf := &byteBuffer{}
	stderrBuf := &byteBuffer{}
	cmd.Stdout = stdoutBuf
	cmd.Stderr = stderrBuf
	err := cmd.Run()
	stdout = stdoutBuf.b
	stderr = stderrBuf.b

	if ctx.Err() != nil {
		return string(stdout), string(stderr), 1, ctx.Err()
	}
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// Non-zero exit is not an error to us: return the code.
			return string(stdout), string(stderr), exitErr.ExitCode(), nil
		}
		// Process failed to start (e.g. docker not installed).
		return string(stdout), string(stderr), 1, err
	}
	return string(stdout), string(stderr), 0, nil
}

type byteBuffer struct{ b []byte }

func (w *byteBuffer) Write(p []byte) (int, error) {
	w.b = append(w.b, p...)
	return len(p), nil
}

// RealFileStore is a node-fs-equivalent filesystem surface over the OS.
type RealFileStore struct{}

func (RealFileStore) ReadFile(path string) (string, error) {
	b, err := os.ReadFile(path)
	return string(b), err
}

func (RealFileStore) WriteFile(path, content string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content), 0o644)
}

func (RealFileStore) Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func (RealFileStore) MkdirAll(path string) error {
	return os.MkdirAll(path, 0o755)
}

// RemoveAll is a no-op when the path is absent (os.RemoveAll already is).
func (RealFileStore) RemoveAll(path string) error {
	return os.RemoveAll(path)
}

// ReadDir returns the entry names, or an empty slice when the path is absent.
func (RealFileStore) ReadDir(path string) ([]string, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []string{}, nil
		}
		return nil, err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	return names, nil
}

// UmbraHome resolves UMBRA_HOME: env override, else ~/.umbra.
func UmbraHome() string {
	if h := os.Getenv("UMBRA_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil {
		home = "."
	}
	return filepath.Join(home, ".umbra")
}

// RealDeps assembles the real boundary (os/exec + os fs + ~/.umbra) for the CLI
// and MCP entrypoints.
func RealDeps() Deps {
	return Deps{Exec: RealExecutor{}, FS: RealFileStore{}, Home: UmbraHome()}
}
