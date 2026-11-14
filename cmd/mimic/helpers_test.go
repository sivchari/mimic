package main

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	binOnce sync.Once
	binDir  string
	binPath string
	binErr  error
)

// TestMain removes the mimic binary that mimicBinary builds.
func TestMain(m *testing.M) {
	code := m.Run()

	if binDir != "" {
		_ = os.RemoveAll(binDir)
	}

	os.Exit(code)
}

// mimicBinary builds the mimic binary once per test run and returns its path.
func mimicBinary(t *testing.T) string {
	t.Helper()

	binOnce.Do(func() {
		binDir, binErr = os.MkdirTemp("", "mimic-bin-")
		if binErr != nil {
			return
		}

		binPath = filepath.Join(binDir, "mimic")
		if out, err := goCmd(".", "build", "-o", binPath, "."); err != nil {
			binErr = fmt.Errorf("build mimic: %w\n%s", err, out)
		}
	})

	if binErr != nil {
		t.Fatal(binErr)
	}

	return binPath
}

// result is the outcome of one in-process mimic invocation.
type result struct {
	code   int
	stdout string
	stderr string
}

// runMimic runs the CLI in-process with dir as the working directory.
func runMimic(dir string, args ...string) result {
	var out, errb bytes.Buffer

	a := &app{dir: dir, stdout: &out, stderr: &errb, getenv: func(string) string { return "" }}
	code := a.run(args)

	return result{code: code, stdout: out.String(), stderr: errb.String()}
}

// copyTree copies the directory tree src into a fresh temporary directory and returns it.
func copyTree(t *testing.T, src string) string {
	t.Helper()

	dst := t.TempDir()

	err := filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}

		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o750)
		}

		data, err := os.ReadFile(filepath.Clean(p))
		if err != nil {
			return err
		}

		return os.WriteFile(target, data, 0o600)
	})
	if err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}

	return dst
}

// writeFile writes content to dir/name, creating parent directories.
func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()

	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// readFile returns the content of dir/name.
func readFile(t *testing.T, dir, name string) string {
	t.Helper()

	b, err := os.ReadFile(filepath.Clean(filepath.Join(dir, name)))
	if err != nil {
		t.Fatal(err)
	}

	return string(b)
}

// goCmd runs the go tool in dir and returns its combined output.
func goCmd(dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(context.Background(), "go", args...)
	cmd.Dir = dir

	out, err := cmd.CombinedOutput()

	return string(out), err
}

// readLines returns the non-empty lines of a testdata list file.
func readLines(t *testing.T, path string) []string {
	t.Helper()

	b, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		t.Fatal(err)
	}

	var lines []string

	for l := range strings.SplitSeq(string(b), "\n") {
		if l = strings.TrimSpace(l); l != "" {
			lines = append(lines, l)
		}
	}

	return lines
}

// splitSpec splits "import/path.Name" at the last dot.
func splitSpec(spec string) (string, string) {
	i := strings.LastIndex(spec, ".")

	return spec[:i], spec[i+1:]
}

// isoName turns a spec into a directory name that is a valid Go package path element.
func isoName(spec string) string {
	return strings.NewReplacer("/", "_", ".", "_").Replace(spec)
}
