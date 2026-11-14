package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/sivchari/mimic"
)

func TestRun_Basics(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		args       []string
		code       int
		stdoutHas  string
		stderrHas  string
		stdoutOnly bool
	}{
		{name: "no arguments", code: exitError, stderrHas: "usage:"},
		{name: "unknown subcommand", args: []string{"nope"}, code: exitError, stderrHas: `unknown subcommand "nope"`},
		{name: "version", args: []string{"version"}, code: exitOK, stdoutHas: mimic.Version},
		{name: "help", args: []string{"help"}, code: exitOK, stdoutHas: "mimic gen"},
		{name: "gen without interface", args: []string{"gen", "./x"}, code: exitError, stderrHas: "need <importpath|./dir>"},
		{name: "gen bad flag", args: []string{"gen", "-nope"}, code: exitError, stderrHas: "flag provided but not defined"},
		{name: "gen -h", args: []string{"gen", "-h"}, code: exitOK, stderrHas: "-pkg"},
		{name: "gen -h lists -skip-ensure", args: []string{"gen", "-h"}, code: exitOK, stderrHas: "-skip-ensure"},
		{name: "help lists -skip-ensure", args: []string{"help"}, code: exitOK, stdoutHas: "-skip-ensure"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := runMimic(t.TempDir(), tt.args...)
			if res.code != tt.code {
				t.Fatalf("code %d, want %d (stderr %q)", res.code, tt.code, res.stderr)
			}

			if !strings.Contains(res.stdout, tt.stdoutHas) || !strings.Contains(res.stderr, tt.stderrHas) {
				t.Fatalf("stdout %q, stderr %q; want %q / %q", res.stdout, res.stderr, tt.stdoutHas, tt.stderrHas)
			}
		})
	}
}

func TestGen_Errors(t *testing.T) {
	t.Parallel()

	root := copyTree(t, suiteDir)

	tests := []struct {
		name string
		args []string
		want string
	}{
		{"unknown interface", []string{"gen", "example.com/suite/domain", "Nope"}, "domain.Nope not found"},
		{"unknown package", []string{"gen", "example.com/suite/nope", "X"}, `cannot find package "example.com/suite/nope"`},
		{"unknown directory", []string{"gen", "./nope", "X"}, "nope"},
		{"unexported method", []string{"gen", "./hcases", "PrivMethod"}, "hcases.PrivMethod: unexported method priv cannot be implemented outside its package"},
		{"unexported type", []string{"gen", "./domain", "U"}, "domain.U: domain.unexp is unexported; generate the mock in package domain"},
		{"not an interface", []string{"gen", "time", "Month"}, "time.Month is not an interface"},
		{"same mock name", []string{"gen", "example.com/suite/domain", "Repo", "Repo"}, "would both be generated as RepoMock"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			res := runMimic(root, tt.args...)
			if res.code != exitError || !strings.Contains(res.stderr, tt.want) || !strings.HasPrefix(res.stderr, "mimic: ") ||
				strings.Count(res.stderr, "\n") != 1 {
				t.Fatalf("code %d, stderr %q; want a one-line error containing %q", res.code, res.stderr, tt.want)
			}

			if res.stdout != "" {
				t.Fatalf("an error must not write a partial result, got %q", res.stdout)
			}
		})
	}
}

func TestGen_Stdout(t *testing.T) {
	t.Parallel()

	root := copyTree(t, suiteDir)

	res := runMimic(root, "gen", "./domain", "Repo")
	if res.code != exitOK {
		t.Fatalf("gen failed: %s", res.stderr)
	}

	if !strings.Contains(res.stdout, "\npackage mocks\n") {
		t.Fatalf("default package should be mocks:\n%.200s", res.stdout)
	}

	// A directory argument and an import path produce the same output.
	other := runMimic(root, "gen", "example.com/suite/domain", "Repo")
	if other.stdout != res.stdout {
		t.Fatal("./domain and example.com/suite/domain differ")
	}
}

func TestGen_PackageName(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		out     string
		flagPkg string
		env     string
		want    string
	}{
		{name: "flag wins", out: "domain/x.go", flagPkg: "custom", want: "custom"},
		{name: "existing package in the output directory", out: "domain/x.go", want: "domain"},
		{name: "new directory", out: "fresh/x.go", want: "mocks"},
		{name: "GOPACKAGE in the working directory", out: "x.go", env: "fromenv", want: "fromenv"},
		{name: "GOPACKAGE elsewhere is ignored", out: "fresh/x.go", env: "fromenv", want: "mocks"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			dir := copyTree(t, suiteDir)

			var out, errb bytes.Buffer

			a := &app{dir: dir, stdout: &out, stderr: &errb, getenv: func(k string) string {
				if k == "GOPACKAGE" {
					return tt.env
				}

				return ""
			}}

			args := []string{"gen", "-o", tt.out}
			if tt.flagPkg != "" {
				args = append(args, "-pkg", tt.flagPkg)
			}

			if code := a.run(append(args, "example.com/suite/other", "Base")); code != exitOK {
				t.Fatalf("gen failed: %s", errb.String())
			}

			if src := readFile(t, dir, tt.out); !strings.Contains(src, "\npackage "+tt.want+"\n") {
				t.Fatalf("want package %s:\n%.300s", tt.want, src)
			}
		})
	}
}

func TestGen_SkipEnsure(t *testing.T) {
	t.Parallel()

	root := copyTree(t, suiteDir)

	with := runMimic(root, "gen", "./domain", "Repo")
	if with.code != exitOK || !strings.Contains(with.stdout, "var _ domain.Repo = (*RepoMock)(nil)") ||
		!strings.Contains(with.stdout, `"example.com/suite/domain"`) {
		t.Fatalf("default output should check the interface (code %d):\n%s%s", with.code, with.stdout, with.stderr)
	}

	without := runMimic(root, "gen", "-skip-ensure", "./domain", "Repo")
	if without.code != exitOK {
		t.Fatalf("gen -skip-ensure failed: %s", without.stderr)
	}

	for _, bad := range []string{"var _ domain.Repo", "If this fails to compile", `"example.com/suite/domain"`} {
		if strings.Contains(without.stdout, bad) {
			t.Errorf("-skip-ensure output must not contain %q:\n%s", bad, without.stdout)
		}
	}

	// Without the check and its import the file must still compile on its own.
	for _, names := range [][]string{{"Repo"}, {"GRepo"}, {"Repo", "Al", "Multi5"}} {
		args := append([]string{"gen", "-skip-ensure", "-o", "skip/zz_gen.go", "-pkg", "skip", "./domain"}, names...)
		if res := runMimic(root, args...); res.code != exitOK {
			t.Fatalf("gen %v failed: %s", names, res.stderr)
		}

		if out, err := goCmd(root, "vet", "./skip"); err != nil {
			t.Fatalf("go vet %v: %v\n%s", names, err, out)
		}
	}
}
