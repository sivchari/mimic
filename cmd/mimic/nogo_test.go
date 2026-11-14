package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

const modresDir = "../../testdata/modres"

// TestE2E_NoGoCommand generates a mock whose parameter type lives in a module cache, with no go
// binary reachable. It proves that mimic resolves packages without starting the go command.
func TestE2E_NoGoCommand(t *testing.T) {
	t.Parallel()

	if runtime.GOOS == "windows" {
		t.Skip("the go shim is a shell script")
	}

	bin := mimicBinary(t)

	root := copyTree(t, modresDir)

	// A go on PATH that records every start and fails.
	shims := t.TempDir()
	started := filepath.Join(shims, "started")
	writeFile(t, shims, "go", "#!/bin/sh\necho \"$@\" >> "+started+"\nexit 1\n")

	if err := os.Chmod(filepath.Join(shims, "go"), 0o755); err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		name string
		path string
	}{
		{"empty PATH", ""},
		{"failing go on PATH", shims},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out := filepath.Join(t.TempDir(), "mocks", "store_mock.go")

			cmd := exec.CommandContext(t.Context(), bin, "gen", "-o", out, "-pkg", "mocks", ".", "Store")
			cmd.Dir = root
			cmd.Env = []string{
				"PATH=" + tt.path,
				"GOROOT=" + runtime.GOROOT(),
				"GOMODCACHE=" + filepath.Join(root, "modcache"),
				"GOENV=off",
				"HOME=" + t.TempDir(),
			}

			if b, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("mimic gen: %v\n%s", err, b)
			}

			src, err := os.ReadFile(filepath.Clean(out))
			if err != nil {
				t.Fatal(err)
			}

			for _, want := range []string{`"example.com/cached"`, `"example.com/Upper"`, `"example.com/localrep"`, "cached.Item", "upper.Doc", "localrep.Piece"} {
				if !strings.Contains(string(src), want) {
					t.Errorf("generated mock lacks %s:\n%s", want, src)
				}
			}

			if _, err := os.Stat(started); err == nil {
				b, _ := os.ReadFile(filepath.Clean(started))
				t.Fatalf("mimic started the go command: %s", b)
			}
		})
	}
}

// TestGen_MissingModule reports the one action that fixes a module that is not downloaded.
func TestGen_MissingModule(t *testing.T) {
	t.Parallel()

	root := copyTree(t, modresDir)

	var errb strings.Builder

	a := &app{dir: root, stdout: &strings.Builder{}, stderr: &errb, getenv: func(k string) string {
		switch k {
		case "GOMODCACHE":
			return filepath.Join(root, "empty-cache")
		case "GOROOT":
			return runtime.GOROOT()
		case "GOENV":
			return "off"
		}

		return ""
	}}

	if code := a.run([]string{"gen", ".", "Store"}); code != exitError {
		t.Fatalf("code %d, want %d", code, exitError)
	}

	if want := `is not in the module cache; run "go mod download"`; !strings.Contains(errb.String(), want) {
		t.Fatalf("stderr %q should contain %q", errb.String(), want)
	}
}
