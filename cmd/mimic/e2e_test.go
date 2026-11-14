package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const (
	suiteDir   = "../../testdata/suite"
	stdDir     = "../../testdata/std"
	callersDir = "../../testdata/callers"
)

// refusedSuite lists the interfaces that must be refused, with the exact message. Their method
// signatures name types that cannot be written from the output package.
var refusedSuite = map[string]string{
	"example.com/suite/domain.U":           "domain.U: domain.unexp is unexported; generate the mock in package domain",
	"example.com/suite/hidden.Sec":         "hidden.Sec: hidden.box is unexported; generate the mock in package hidden",
	"example.com/suite/hidden.GenSec":      "hidden.GenSec: hidden.box is unexported; generate the mock in package hidden",
	"example.com/suite/hcases.Embeds2":     "hcases.Embeds2: hidden.box is unexported; generate the mock in package hidden",
	"example.com/suite/hcases.StructLit":   "hcases.StructLit: struct literal has unexported field a; generate the mock in package hcases",
	"example.com/suite/hcases.IfaceLit":    "hcases.IfaceLit: interface literal has unexported method priv; generate the mock in package hcases",
	"example.com/suite/hcases.PrivMethod":  "hcases.PrivMethod: unexported method priv cannot be implemented outside its package",
	"example.com/suite/hcases.GenEmbed":    "hcases.GenEmbed: hidden.box is unexported; generate the mock in package hidden",
	"example.com/suite/hcases.GenEmbedInt": "hcases.GenEmbedInt: hidden.box is unexported; generate the mock in package hidden",
}

// genericArgs are the type arguments the e2e caller uses to instantiate mocks of generic interfaces.
var genericArgs = map[string]string{
	"domain.GRepo":   "[int]",
	"domain.GC":      "[string, int]",
	"tricky.GA":      "[int]",
	"tricky.GB":      "[int]",
	"tricky.GShadow": "[int]",
	"tricky.GK":      "[string, int]",
}

// callerSource builds the _test.go file that calls every method of the generated mock.
func callerSource(spec string) string {
	path, name := splitSpec(spec)
	args := genericArgs[filepath.Base(path)+"."+name]

	return "package m\n\nimport \"testing\"\n\nfunc TestInvoke(t *testing.T) {\n\tinvoke(t, &" + name + "Mock" + args + "{})\n}\n"
}

// genBuildVetRun generates the mock of spec into an isolated package and builds, vets and runs
// a caller that invokes every method. It returns the generated source.
func genBuildVetRun(t *testing.T, root, spec string, invoke []byte) string {
	t.Helper()

	path, name := splitSpec(spec)
	rel := filepath.Join("iso", isoName(spec))

	if res := runMimic(root, "gen", "-o", filepath.Join(rel, "zz_gen.go"), "-pkg", "m", path, name); res.code != 0 {
		t.Fatalf("gen failed (%d): %s", res.code, res.stderr)
	}

	writeFile(t, root, filepath.Join(rel, "invoke_test.go"), string(invoke))
	writeFile(t, root, filepath.Join(rel, "caller_test.go"), callerSource(spec))

	pkg := "./" + filepath.ToSlash(rel)
	for _, args := range [][]string{{"build", pkg}, {"vet", pkg}, {"test", "-count=1", pkg}} {
		if out, err := goCmd(root, args...); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}

	return readFile(t, root, filepath.Join(rel, "zz_gen.go"))
}

// assertRefused checks that generating spec fails with exactly want and leaves no file behind.
func assertRefused(t *testing.T, root, spec, want, out string) {
	t.Helper()

	path, name := splitSpec(spec)
	res := runMimic(root, "gen", "-o", out, "-pkg", "m", path, name)

	if res.code == 0 || res.stderr != "mimic: "+want+"\n" || res.stdout != "" {
		t.Fatalf("want refusal %q, got code %d stdout %q stderr %q", want, res.code, res.stdout, res.stderr)
	}

	if _, err := os.Stat(filepath.Join(root, out)); err == nil {
		t.Fatal("a refused interface must not leave a generated file")
	}
}

// TestE2E_Suite generates every hard interface in isolation. Interfaces that can be written from
// the output package must build, vet and run; the others must be refused with the exact message.
func TestE2E_Suite(t *testing.T) {
	t.Parallel()

	root := copyTree(t, suiteDir)

	invoke, err := os.ReadFile(filepath.Join(callersDir, "invoke.go.txt"))
	if err != nil {
		t.Fatal(err)
	}

	specs := readLines(t, filepath.Join(suiteDir, "interfaces.txt"))
	if len(specs) != 47 {
		t.Fatalf("interfaces.txt lists %d interfaces, want 47", len(specs))
	}

	var passed, refused atomic.Int32

	t.Run("cases", func(t *testing.T) {
		for _, spec := range specs {
			t.Run(spec, func(t *testing.T) {
				t.Parallel()

				if want, ok := refusedSuite[spec]; ok {
					assertRefused(t, root, spec, want, filepath.Join("iso", isoName(spec), "zz_gen.go"))
					refused.Add(1)

					return
				}

				genBuildVetRun(t, root, spec, invoke)
				passed.Add(1)
			})
		}
	})

	t.Logf("hard cases: %d of %d pass, %d refused", passed.Load(), len(specs), refused.Load())

	if int(refused.Load()) != len(refusedSuite) || int(passed.Load()) != len(specs)-len(refusedSuite) {
		t.Fatalf("passed %d, refused %d; want %d refusals", passed.Load(), refused.Load(), len(refusedSuite))
	}
}

// TestE2E_Resolved covers types the generator writes by resolving unexported aliases and constants
// and internal/ aliases through the AST. The mocks must be concrete and carry the resolved text.
func TestE2E_Resolved(t *testing.T) {
	t.Parallel()

	root := copyTree(t, suiteDir)

	invoke, err := os.ReadFile(filepath.Join(callersDir, "invoke.go.txt"))
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		spec string
		has  []string
		not  []string
	}{
		{
			spec: "example.com/suite/consts.Arr",
			has:  []string{"BFunc func(a [2]int) error", "AFunc func(a [consts.Size]int, b [consts.Size * 2]byte) [consts.Size + 1]string"},
		},
		{
			spec: "example.com/suite/hidden.Sized",
			has:  []string{"FixedFunc func(a [3]int) [3]string"},
			not:  []string{"nn"},
		},
		{
			spec: "example.com/suite/resolve.Aliased",
			has: []string{
				"GetFunc   func(id string) (model.User, error)",
				"ListFunc  func(ids []string) []model.User",
				"EachFunc  func(h func(u model.User) error) error",
				"SeenFunc  func(s map[string]struct{}) bool",
				"ChansFunc func(c chan string, d <-chan []string)",
			},
		},
		{
			spec: "example.com/suite/resolve.Consts",
			has: []string{
				"ArrFunc   func(a [10]int, b [4]byte) [6]string",
				"MixedFunc func(a [6]int) [len(\"abc\")]byte",
				"ConvFunc  func(a [4]int) error",
				"NamedFunc func(a [resolve.Size * 2]int) [4]bool",
			},
		},
		{
			spec: "example.com/suite/lib.Svc",
			has: []string{
				"GetFunc   func(ref model.User) (map[model.UserID]*model.User, error)",
				"FixedFunc func(a [3]int) error",
			},
			not: []string{"internal"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.spec, func(t *testing.T) {
			t.Parallel()

			src := genBuildVetRun(t, root, tt.spec, invoke)

			for _, want := range tt.has {
				if !strings.Contains(src, want) {
					t.Errorf("output lacks %q", want)
				}
			}

			for _, bad := range tt.not {
				if strings.Contains(src, bad) {
					t.Errorf("output must not contain %q", bad)
				}
			}
		})
	}
}

// TestE2E_Internal covers an internal/ package: its defined types can only be named from inside
// the internal tree's parent.
func TestE2E_Internal(t *testing.T) {
	t.Parallel()

	root := copyTree(t, suiteDir)

	assertRefused(t, root, "example.com/suite/lib.Pipe",
		"lib.Pipe: example.com/suite/lib/internal/types is internal; generate the mock inside example.com/suite/lib",
		"iso/pipe/zz_gen.go")

	// Inside the parent of the internal tree the types are importable and written by name.
	for _, name := range []string{"Svc", "Pipe"} {
		out := "lib/mocks/" + strings.ToLower(name) + "_gen.go"
		if res := runMimic(root, "gen", "-o", out, "-pkg", "mocks", "example.com/suite/lib", name); res.code != 0 {
			t.Fatalf("gen %s failed (%d): %s", name, res.code, res.stderr)
		}
	}

	if src := readFile(t, root, "lib/mocks/pipe_gen.go"); !strings.Contains(src, "OpenFunc func() types.Conn") {
		t.Errorf("an importable internal type should be written by name:\n%s", src)
	}

	if out, err := goCmd(root, "vet", "./lib/..."); err != nil {
		t.Fatalf("go vet: %v\n%s", err, out)
	}
}

// refusedStd lists the std specs that are not mockable, with the exact message.
var refusedStd = map[string]string{
	"go/token.Pos":             "token.Pos is not an interface",
	"text/template/parse.Node": "parse.Node: unexported method tree cannot be implemented outside its package",
	"reflect.Type":             "reflect.Type: unexported method common cannot be implemented outside its package",
	"go/ast.Expr":              "ast.Expr: unexported method exprNode cannot be implemented outside its package",
	"testing.TB":               "testing.TB: unexported method private cannot be implemented outside its package",
	"html/template.FuncMap":    "template.FuncMap is not an interface",
	"time.Month":               "time.Month is not an interface",
	"unicode/utf8.RuneBuf":     "unicode/utf8.RuneBuf not found",
}

// TestE2E_Std generates mocks of standard library interfaces and vets them.
func TestE2E_Std(t *testing.T) {
	t.Parallel()

	root := copyTree(t, stdDir)
	specs := readLines(t, filepath.Join(stdDir, "specs.txt"))

	var passed atomic.Int32

	t.Run("specs", func(t *testing.T) {
		for _, spec := range specs {
			want, refused := refusedStd[strings.TrimPrefix(spec, "!")]
			if refused != strings.HasPrefix(spec, "!") {
				t.Fatalf("%s: specs.txt and refusedStd disagree", spec)
			}

			spec = strings.TrimPrefix(spec, "!")

			t.Run(spec, func(t *testing.T) {
				t.Parallel()

				path, name := splitSpec(spec)
				rel := filepath.Join("iso", isoName(spec))
				res := runMimic(root, "gen", "-o", filepath.Join(rel, "zz_gen.go"), "-pkg", "m", path, name)

				if refused {
					if res.code == 0 || res.stderr != "mimic: "+want+"\n" {
						t.Fatalf("want refusal %q, got code %d stderr %q", want, res.code, res.stderr)
					}

					return
				}

				if res.code != 0 {
					t.Fatalf("gen failed (%d): %s", res.code, res.stderr)
				}

				if out, err := goCmd(root, "vet", "./"+filepath.ToSlash(rel)); err != nil {
					t.Fatalf("go vet: %v\n%s", err, out)
				}

				passed.Add(1)
			})
		}
	})

	t.Logf("std interfaces passing vet: %d", passed.Load())

	if passed.Load() != 63 {
		t.Fatalf("%d std interfaces pass vet, want 63", passed.Load())
	}
}

// TestE2E_OneFile generates several interfaces into one file.
func TestE2E_OneFile(t *testing.T) {
	t.Parallel()

	root := copyTree(t, suiteDir)
	res := runMimic(root, "gen", "-o", "multi/zz_gen.go", "-pkg", "multi",
		"example.com/suite/domain", "Repo", "Multi5", "Var", "Embeds", "GRepo", "Al", "Names", "GNames", "GNamesString")

	if res.code != 0 {
		t.Fatalf("gen failed (%d): %s", res.code, res.stderr)
	}

	if out, err := goCmd(root, "vet", "./multi"); err != nil {
		t.Fatalf("go vet: %v\n%s", err, out)
	}
}

// TestE2E_TwoFiles puts the output of two separate mimic runs into one package: nothing the
// generated files declare may collide, and every mock must still work.
func TestE2E_TwoFiles(t *testing.T) {
	t.Parallel()

	root := copyTree(t, suiteDir)

	for _, g := range []struct {
		out   string
		names []string
	}{
		{"two/a_gen.go", []string{"Repo", "Al", "GRepo"}},
		{"two/b_gen.go", []string{"Var", "Multi5", "Embeds", "Names"}},
	} {
		args := append([]string{"gen", "-o", g.out, "-pkg", "m", "example.com/suite/domain"}, g.names...)
		if res := runMimic(root, args...); res.code != 0 {
			t.Fatalf("gen %s failed (%d): %s", g.out, res.code, res.stderr)
		}
	}

	invoke, err := os.ReadFile(filepath.Join(callersDir, "invoke.go.txt"))
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, root, "two/invoke_test.go", string(invoke))
	writeFile(t, root, "two/caller_test.go", `package m

import "testing"

func TestInvokeBothFiles(t *testing.T) {
	invoke(t, &RepoMock{})
	invoke(t, &AlMock{})
	invoke(t, &GRepoMock[int]{})
	invoke(t, &VarMock{})
	invoke(t, &Multi5Mock{})
	invoke(t, &EmbedsMock{})
	invoke(t, &NamesMock{})
}
`)

	for _, args := range [][]string{{"build", "./two"}, {"vet", "./two"}, {"test", "-count=1", "./two"}} {
		if out, err := goCmd(root, args...); err != nil {
			t.Fatalf("go %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
}

// TestE2E_GoGenerate runs the binary from a go:generate directive, as users do: the working
// directory is the package directory and the mock lands in the interface's own package.
func TestE2E_GoGenerate(t *testing.T) {
	t.Parallel()

	bin := mimicBinary(t)

	root := copyTree(t, suiteDir)
	writeFile(t, root, "domain/gen.go", "package domain\n\n//go:generate mimic gen -o repo_mock.go . Repo Var U\n")
	writeFile(t, root, "domain/repo_test.go", `package domain

import (
	"context"
	"testing"

	"example.com/suite/model"
)

func TestRepoMock(t *testing.T) {
	m := &RepoMock{GetFunc: func(_ context.Context, id string) (*model.User, error) { return &model.User{ID: id}, nil }}

	var r Repo = m
	if u, _ := r.Get(context.Background(), "x"); u.ID != "x" {
		t.Fatal(u)
	}

	if c := m.GetCalls(); len(c) != 1 || c[0].ID != "x" {
		t.Fatal(c)
	}
}

// U has an unexported parameter type: inside its own package the mock can name it.
func TestUnexportedTypeInOwnPackage(t *testing.T) {
	var got int

	m := &UMock{UseFunc: func(u unexp) error { got = u.v; return nil }}

	if err := U(m).Use(MakeUnexp()); err != nil || got != 7 {
		t.Fatal(got, err)
	}
}
`)

	cmd := exec.CommandContext(t.Context(), "go", "generate", "./domain")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "PATH="+filepath.Dir(bin)+string(os.PathListSeparator)+os.Getenv("PATH"))

	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go generate: %v\n%s", err, out)
	}

	src := readFile(t, root, "domain/repo_mock.go")
	if !strings.HasPrefix(src, "// Code generated by mimic. DO NOT EDIT.\n// source: example.com/suite/domain.Repo\n") ||
		!strings.Contains(src, "\npackage domain\n") {
		t.Fatalf("unexpected header or package:\n%.300s", src)
	}

	if strings.Contains(src, "example.com/suite/domain\"") {
		t.Fatal("a mock in the interface's own package must not import it")
	}

	if out, err := goCmd(root, "test", "-count=1", "./domain"); err != nil {
		t.Fatalf("go test: %v\n%s", err, out)
	}
}

// TestE2E_Semantics runs hand-written tests against generated mocks: recording, the nil-Func
// panic, variadics, sanitized names, resolved aliases and concurrent use under the race detector.
func TestE2E_Semantics(t *testing.T) {
	t.Parallel()

	root := copyTree(t, suiteDir)

	for _, g := range []struct {
		out, pkg, path string
		names          []string
	}{
		{"semantic/dm/zz_gen.go", "dm", "example.com/suite/domain", []string{"Repo", "Multi5", "Var", "Names", "Embeds"}},
		{"semantic/rm/zz_gen.go", "rm", "example.com/suite/resolve", []string{"Aliased", "Consts"}},
	} {
		args := append([]string{"gen", "-o", g.out, "-pkg", g.pkg, g.path}, g.names...)
		if res := runMimic(root, args...); res.code != 0 {
			t.Fatalf("gen %v failed: %s", g.names, res.stderr)
		}
	}

	src, err := os.ReadFile(filepath.Join(callersDir, "semantic.go.txt"))
	if err != nil {
		t.Fatal(err)
	}

	writeFile(t, root, "semantic/semantic_test.go", string(src))

	if out, err := goCmd(root, "test", "-race", "-count=1", "./semantic/..."); err != nil {
		t.Fatalf("go test: %v\n%s", err, out)
	}
}
