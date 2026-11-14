package load_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sivchari/mimic/internal/load"
)

const suite = "../../testdata/suite"

func newLoader(t *testing.T) *load.Loader {
	t.Helper()

	l, err := load.New(suite)
	if err != nil {
		t.Fatal(err)
	}

	return l
}

func TestFailfRecover(t *testing.T) {
	t.Parallel()

	f := func() (err error) {
		defer load.Recover(&err)

		load.Failf("boom %d", 7)

		return nil
	}

	err := f()

	var le *load.Error
	if !errors.As(err, &le) || le.Msg != "boom 7" || err.Error() != "boom 7" {
		t.Fatalf("got %v", err)
	}

	g := func() (err error) {
		defer load.Recover(&err)

		return nil
	}
	if err := g(); err != nil {
		t.Fatalf("no panic must leave err nil, got %v", err)
	}
}

func TestRecover_OtherPanicsPropagate(t *testing.T) {
	t.Parallel()

	defer func() {
		if r := recover(); r != "other" {
			t.Fatalf("recovered %v, want the original panic", r)
		}
	}()

	func() {
		var err error

		defer load.Recover(&err)

		panic("other")
	}()
}

func TestLoader_Package(t *testing.T) {
	t.Parallel()

	l := newLoader(t)
	p := l.Package("example.com/suite/domain")

	if p.Name != "domain" || p.Types["Repo"] == nil || !p.Names["MakeUnexp"] || !p.Names["unexp"] {
		t.Fatalf("unexpected package contents: %+v", p.Names)
	}

	if l.Package("example.com/suite/domain") != p {
		t.Fatal("packages must be cached")
	}

	if got := l.PackageName("example.com/suite/pkgdiff"); got != "realname" {
		t.Fatalf("package clause name = %q, want realname", got)
	}

	if got := l.Package("io").Name; got != "io" {
		t.Fatalf("standard library package: %q", got)
	}
}

func TestLoader_Package_Missing(t *testing.T) {
	t.Parallel()

	l := newLoader(t)

	var err error

	func() {
		defer load.Recover(&err)

		l.Package("example.com/suite/nope")
	}()

	if err == nil || !strings.Contains(err.Error(), `cannot find package "example.com/suite/nope"`) {
		t.Fatalf("got %v", err)
	}
}

func TestLoader_ImportPath(t *testing.T) {
	t.Parallel()

	l := newLoader(t)

	// tricky/a.go imports example.com/suite/model as m; tricky/b.go imports .../other as m.
	var a, b *load.File

	for _, f := range l.Package("example.com/suite/tricky").Files {
		switch filepath.Base(l.Fset.Position(f.AST.Pos()).Filename) {
		case "a.go":
			a = f
		case "b.go":
			b = f
		}
	}

	if got, _ := l.ImportPath(a, "m"); got != "example.com/suite/model" {
		t.Errorf("a.go: m = %q", got)
	}

	if got, _ := l.ImportPath(b, "m"); got != "example.com/suite/other" {
		t.Errorf("b.go: m = %q", got)
	}

	if _, ok := l.ImportPath(a, "nope"); ok {
		t.Error("unknown qualifier resolved")
	}

	// An unaliased import is found by its package clause name, not its path element.
	for _, f := range l.Package("example.com/suite/tricky").Files {
		for _, is := range f.Imports {
			if is.Path == "example.com/suite/pkgdiff" {
				if got, ok := l.ImportPath(f, "realname"); !ok || got != is.Path {
					t.Errorf("realname = %q, %v", got, ok)
				}
			}
		}
	}
}

func TestLoader_DotLookup(t *testing.T) {
	t.Parallel()

	l := newLoader(t)

	var f *load.File

	for _, c := range l.Package("example.com/suite/trickydot").Files {
		if len(c.Dots) > 0 {
			f = c
		}
	}

	if f == nil {
		t.Fatal("no dot import found in trickydot")
	}

	if got, ok := l.DotLookup(f, "Thing"); !ok || got != "example.com/suite/other" {
		t.Errorf("Thing = %q, %v", got, ok)
	}

	if _, ok := l.DotLookup(f, "nothing"); ok {
		t.Error("unexported name must not resolve through a dot import")
	}
}

func TestLoader_ResolveDir(t *testing.T) {
	t.Parallel()

	l := newLoader(t)

	got, err := l.ResolveDir("./domain")
	if err != nil || got != "example.com/suite/domain" {
		t.Fatalf("got %q, %v", got, err)
	}

	if _, err := l.ResolveDir("./does-not-exist"); err == nil {
		t.Fatal("want an error for a missing directory")
	}
}

func TestLoader_Pos(t *testing.T) {
	t.Parallel()

	l := newLoader(t)
	td := l.Package("example.com/suite/domain").Types["Repo"]

	if got := l.Pos(td.Spec.Pos()); !strings.HasPrefix(got, "domain"+string(filepath.Separator)+"domain.go:") {
		t.Fatalf("position %q should be relative to the loader's directory", got)
	}
}

func TestModulePath(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	if err := os.MkdirAll(filepath.Join(dir, "a", "b"), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n\ngo 1.26\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	tests := map[string]string{
		filepath.Join(dir, "x.go"):           "example.com/m",
		filepath.Join(dir, "a", "b", "x.go"): "example.com/m/a/b",
		filepath.Join(dir, "new", "x.go"):    "example.com/m/new",
	}

	for file, want := range tests {
		if got := load.ModulePath(file); got != want {
			t.Errorf("ModulePath(%s) = %q, want %q", file, got, want)
		}
	}

	if got := load.ModulePath(filepath.Join(os.TempDir(), "mimic-no-such-module", "x.go")); got != "" {
		t.Errorf("outside a module: got %q", got)
	}
}

func TestLoader_Consts(t *testing.T) {
	t.Parallel()

	p := newLoader(t).Package("example.com/suite/resolve")

	// k1 repeats the expression of k0 with the next iota.
	k0, k1, k3 := p.Consts["k0"], p.Consts["k1"], p.Consts["k3"]
	if k0 == nil || k1 == nil || k3 == nil || k0.Expr != k1.Expr || k0.Iota != 0 || k1.Iota != 1 || k3.Iota != 3 {
		t.Fatalf("iota constants: %+v %+v %+v", k0, k1, k3)
	}

	if c := p.Consts["total"]; c == nil || c.Iota != 1 || !p.Names["total"] {
		t.Fatalf("total: %+v", c)
	}

	if p.Consts["Aliased"] != nil {
		t.Fatal("a type is not a constant")
	}
}

func TestTypeDecl_IsAlias(t *testing.T) {
	t.Parallel()

	p := newLoader(t).Package("example.com/suite/resolve")

	if !p.Types["id"].IsAlias() || !p.Types["set"].IsAlias() || p.Types["Aliased"].IsAlias() {
		t.Fatal("IsAlias is wrong")
	}
}
