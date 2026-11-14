package rewrite_test

import (
	"go/ast"
	"go/parser"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sivchari/mimic/internal/canon"
	"github.com/sivchari/mimic/internal/load"
	"github.com/sivchari/mimic/internal/rewrite"
)

// module writes files into a temporary module example.com/rw and returns a loader for it.
func module(t *testing.T, files map[string]string) *load.Loader {
	t.Helper()

	dir := t.TempDir()
	files["go.mod"] = "module example.com/rw\n\ngo 1.26\n"

	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
			t.Fatal(err)
		}

		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	l, err := load.New(dir)
	if err != nil {
		t.Fatal(err)
	}

	return l
}

// ctxOf returns a context for the first file of the package at import path.
func ctxOf(l *load.Loader, path string) *canon.Ctx {
	return &canon.Ctx{File: l.Package(path).Files[0], TP: map[string]canon.TypeParam{}}
}

func parse(t *testing.T, src string) ast.Expr {
	t.Helper()

	e, err := parser.ParseExpr(src)
	if err != nil {
		t.Fatalf("%s: %v", src, err)
	}

	return e
}

// write runs w.Type and returns the failure as an error.
func write(t *testing.T, w *rewrite.Writer, src string, c *canon.Ctx) (s string, err error) {
	t.Helper()

	e := parse(t, src)

	defer load.Recover(&err)

	return w.Type(e, c), nil
}

const (
	modelSrc = `package model

type User struct{}
type secret struct{}
type hidden = secret
type id = string
type Exported = User
type List[T any] []T

const sz = 2
const Size = 4
`
	aSrc = "package a\n\nimport m \"example.com/rw/model\"\n\ntype Local struct{}\n"
)

func TestWriter_Type(t *testing.T) {
	t.Parallel()

	l := module(t, map[string]string{"model/model.go": modelSrc, "a/a.go": aSrc})
	c := ctxOf(l, "example.com/rw/a")

	tests := []struct {
		name    string
		outPath string
		src     string
		want    string
	}{
		{"other package", "example.com/rw/out", "m.User", "m.User"},
		{"same package", "example.com/rw/model", "m.User", "User"},
		{"unknown output package", "", "m.User", "m.User"},
		{"local type", "example.com/rw/out", "Local", "a.Local"},
		{"local type in its own package", "example.com/rw/a", "Local", "Local"},
		{"composite", "example.com/rw/out", "map[string][]*m.User", "map[string][]*m.User"},
		{"channel of channel", "example.com/rw/out", "chan (<-chan int)", "chan (<-chan int)"},
		{"func with names", "example.com/rw/out", "func(a, b int, c ...m.User) (e error)", "func(a, b int, c ...m.User) (e error)"},
		{"anonymous interface", "example.com/rw/out", "interface{ Z(int); String() string }", "interface{Z(int); String() string}"},
		{"struct with tag", "example.com/rw/out", "struct{ A int `json:\"a\"` }", "struct{A int `json:\"a\"`}"},
		{"exported alias is kept", "example.com/rw/out", "m.Exported", "m.Exported"},
		{"exported constant is kept", "example.com/rw/out", "[m.Size * 2]int", "[m.Size * 2]int"},
	}

	for _, tt := range tests {
		// The subtests share a Loader, which is not safe for concurrent use.
		t.Run(tt.name, func(t *testing.T) {
			w := rewrite.NewWriter(l, tt.outPath)

			if got, err := write(t, w, tt.src, c); err != nil || got != tt.want {
				t.Fatalf("Type(%s) = %q, %v; want %q", tt.src, got, err, tt.want)
			}
		})
	}
}

func TestWriter_Refusals(t *testing.T) {
	t.Parallel()

	l := module(t, map[string]string{
		"model/model.go":    modelSrc,
		"x/internal/p/p.go": "package p\n\ntype T struct{}\n\nconst N = 1\n",
		"x/a/a.go":          "package a\n\nimport \"example.com/rw/x/internal/p\"\n\ntype I interface{ M(p.T) }\n",
		"std/std.go":        "package std\n\nimport \"internal/abi\"\n\ntype I interface{ M(abi.Type) }\n",
	})

	model, a := ctxOf(l, "example.com/rw/model"), ctxOf(l, "example.com/rw/x/a")
	const out = "example.com/rw/out"

	tests := []struct {
		name string
		out  string
		src  string
		c    *canon.Ctx
		want string
	}{
		{"unexported type", out, "secret", model, "model.secret is unexported; generate the mock in package model"},
		{"unexported type in a composite", out, "map[string][]*secret", model, "model.secret is unexported; generate the mock in package model"},
		{"unexported type as a type argument", out, "List[secret]", model, "model.secret is unexported; generate the mock in package model"},
		{"alias of an unexported type", out, "hidden", model, "model.secret is unexported; generate the mock in package model"},
		{"struct literal", out, "struct{ a int }", model, "struct literal has unexported field a; generate the mock in package model"},
		{"embedded in a struct literal", out, "struct{ *secret }", model, "struct literal has unexported embedded field secret; generate the mock in package model"},
		{"interface literal", out, "interface{ p() }", model, "interface literal has unexported method p; generate the mock in package model"},
		{"internal package", "example.com/rw/y", "p.T", a, "example.com/rw/x/internal/p is internal; generate the mock inside example.com/rw/x"},
		{"internal package, sibling prefix", "example.com/rw/xother", "p.T", a, "example.com/rw/x/internal/p is internal; generate the mock inside example.com/rw/x"},
		{"unresolved identifier", out, "nope", model, `unresolved identifier "nope"`},
		{"unknown qualifier", out, "zz.T", model, `unknown package qualifier "zz"`},
		{"unsupported selector", out, "a.b.C", model, "unsupported selector"},
	}

	for _, tt := range tests {
		// The subtests share a Loader, which is not safe for concurrent use.
		t.Run(tt.name, func(t *testing.T) {
			w := rewrite.NewWriter(l, tt.out)

			if got, err := write(t, w, tt.src, tt.c); err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("Type(%s) = %q, %v; want an error containing %q", tt.src, got, err, tt.want)
			}
		})
	}

	// The standard library's internal packages cannot be imported by anyone else.
	std := ctxOf(l, "example.com/rw/std")

	w := rewrite.NewWriter(l, out)
	if _, err := write(t, w, "abi.Type", std); err == nil || err.Error() != "internal/abi is internal to the standard library" {
		t.Fatalf("got %v", err)
	}
}

func TestWriter_OwnPackage(t *testing.T) {
	t.Parallel()

	l := module(t, map[string]string{"model/model.go": modelSrc})
	c := ctxOf(l, "example.com/rw/model")

	// The output package itself can name its own unexported identifiers.
	w := rewrite.NewWriter(l, "example.com/rw/model")

	for src, want := range map[string]string{
		"[]secret":                 "[]secret",
		"hidden":                   "hidden",
		"[sz]secret":               "[sz]secret",
		"struct{ a int }":          "struct{a int}",
		"interface{ p() }":         "interface{p()}",
		"map[id]*struct{ b int }":  "map[id]*struct{b int}",
		"struct{ *secret; B int }": "struct{*secret; B int}",
	} {
		if got, err := write(t, w, src, c); err != nil || got != want {
			t.Errorf("Type(%s) = %q, %v; want %q", src, got, err, want)
		}
	}
}

func TestWriter_Aliases(t *testing.T) {
	t.Parallel()

	l := module(t, map[string]string{
		"model/model.go": modelSrc,
		"x/x.go": `package x

import m "example.com/rw/model"

type (
	id                = string
	ids               = []id
	user              = m.User
	handler           = func(u user) error
	set[T comparable] = map[T]struct{}
	pair[K, V any]    = map[K][]V
	again             = ids
	rx                = <-chan int
	Pub               = id
)
`,
		"x/internal/in/in.go": `package in

import m "example.com/rw/model"

type Ref = m.User
type Refs = map[string]*Ref
type Conn struct{}
type Secret = Conn
`,
		"y/y.go": "package y\n\nimport \"example.com/rw/x/internal/in\"\n\ntype I interface{ M(in.Ref) }\n",
	})

	x, y := ctxOf(l, "example.com/rw/x"), ctxOf(l, "example.com/rw/y")
	const out = "example.com/rw/out"

	tests := []struct {
		name string
		out  string
		src  string
		c    *canon.Ctx
		want string
	}{
		{"unexported alias", out, "id", x, "string"},
		{"nested alias", out, "again", x, "[]string"},
		{"alias to another package", out, "user", x, "m.User"},
		{"alias in a composite", out, "map[id][]user", x, "map[string][]m.User"},
		{"alias of a func", out, "handler", x, "func(u m.User) error"},
		{"generic alias", out, "set[id]", x, "map[string]struct{}"},
		{"generic alias, two arguments", out, "pair[id, *user]", x, "map[string][]*m.User"},
		{"exported alias of an unexported one is kept", out, "Pub", x, "x.Pub"},
		{"alias is kept in its own package", "example.com/rw/x", "id", x, "id"},
		{"alias of a channel inside a channel", out, "chan rx", x, "chan (<-chan int)"},
		{"internal alias", out, "in.Ref", y, "m.User"},
		{"internal alias of an alias", out, "in.Refs", y, "map[string]*m.User"},
		{"internal alias, importable", "example.com/rw/x/b", "in.Ref", y, "in.Ref"},
		{"internal alias, importable deep", "example.com/rw/x/b/c", "in.Refs", y, "in.Refs"},
	}

	for _, tt := range tests {
		// The subtests share a Loader, which is not safe for concurrent use.
		t.Run(tt.name, func(t *testing.T) {
			w := rewrite.NewWriter(l, tt.out)

			if got, err := write(t, w, tt.src, tt.c); err != nil || got != tt.want {
				t.Fatalf("Type(%s) = %q, %v; want %q", tt.src, got, err, tt.want)
			}
		})
	}

	for src, want := range map[string]string{
		"in.Conn":    "example.com/rw/x/internal/in is internal; generate the mock inside example.com/rw/x",
		"in.Secret":  "example.com/rw/x/internal/in is internal; generate the mock inside example.com/rw/x",
		"set":        "missing type argument for x.set",
		"in.Missing": "is internal",
	} {
		c := x
		if strings.HasPrefix(src, "in.") {
			c = y
		}

		w := rewrite.NewWriter(l, out)

		if got, err := write(t, w, src, c); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("Type(%s) = %q, %v; want an error containing %q", src, got, err, want)
		}
	}
}

func TestWriter_AliasImports(t *testing.T) {
	t.Parallel()

	// Expanding an alias imports what its target needs, not the package that declared the alias.
	l := module(t, map[string]string{
		"model/model.go":      modelSrc,
		"x/internal/in/in.go": "package in\n\nimport m \"example.com/rw/model\"\n\ntype Ref = m.User\n",
		"y/y.go":              "package y\n\nimport \"example.com/rw/x/internal/in\"\n\ntype I interface{ M(in.Ref) }\n",
	})

	w := rewrite.NewWriter(l, "example.com/rw/out")

	if got, err := write(t, w, "in.Ref", ctxOf(l, "example.com/rw/y")); err != nil || got != "m.User" {
		t.Fatalf("got %q, %v", got, err)
	}

	if lines := w.Imports.Lines(); lines != "\tm \"example.com/rw/model\"\n" {
		t.Fatalf("imports:\n%s", lines)
	}
}

func TestWriter_Constants(t *testing.T) {
	t.Parallel()

	l := module(t, map[string]string{
		"c/c.go": `package c

import "example.com/rw/d"

const Size = 4

const (
	base  = 2
	total = base*3 + Size
	half  = total / 4
	neg   = -base + 3
	mask  = ^0 &^ 6 & 7
	big   = 1 << 4
	yes   = total > 5
	str   = "abc"
	f     = 2.0
	conv  = uint8(base) + 1
	other = d.Count * 2
	mixed = d.Count + d.hidden
)

const (
	k0 = iota * 2
	k1
	k2
)

const (
	_ = iota
	s1 = 1 << (10 * iota)
	s2
)

const (
	a, b = iota, iota + 10
	cc, dd
)

type Typed int

const typed Typed = 3

const cyc1 = cyc2
const cyc2 = cyc1
`,
		"d/d.go": "package d\n\nconst Count = 3\nconst hidden = 5\nconst Hidden = hidden\n\ntype T struct{}\n\nfunc F() {}\n",
		"e/e.go": "package e\n\nimport . \"example.com/rw/d\"\n\nconst z = Count + 1\n",
	})

	c := ctxOf(l, "example.com/rw/c")
	const out = "example.com/rw/out"

	tests := []struct {
		src  string
		want string
	}{
		{"[Size]int", "[c.Size]int"}, // reachable constants are written by name
		{"[Size * 2]int", "[c.Size * 2]int"},
		{"[base]int", "[2]int"},
		{"[total]int", "[10]int"},
		{"[total / 4]int", "[2]int"},
		{"[half]int", "[2]int"},
		{"[neg]int", "[1]int"},
		{"[mask]int", "[1]int"},
		{"[big]int", "[16]int"},
		{"[base * (total - 1)]int", "[18]int"},
		{"[Size + base]int", "[6]int"},
		{"[len(str)]int", "[3]int"},
		{"[len(\"abcd\")]int", "[len(\"abcd\")]int"},
		{"[int(f)]int", "[2]int"},
		{"[conv]int", "[3]int"},
		{"[typed]int", "[3]int"},
		{"[Typed(base)]int", "[2]int"},
		{"[int(base) + 1]int", "[3]int"},
		{"[other]int", "[6]int"},
		{"[k0]int", "[0]int"},
		{"[k1]int", "[2]int"},
		{"[k2]int", "[4]int"},
		{"[s1]int", "[1024]int"},
		{"[s2]int", "[1048576]int"},
		{"[a]int", "[0]int"},
		{"[b]int", "[10]int"},
		{"[cc]int", "[1]int"},
		{"[dd]int", "[11]int"},
		{"[d.Count]int", "[d.Count]int"},
		{"[d.hidden]int", "[5]int"},
		{"[d.Hidden]int", "[d.Hidden]int"},
		{"[mixed]int", "[8]int"},
		{"[][base]int", "[][2]int"},
		{"map[string][base]*[total]byte", "map[string][2]*[10]byte"},
	}

	for _, tt := range tests {
		w := rewrite.NewWriter(l, out)

		if got, err := write(t, w, tt.src, c); err != nil || got != tt.want {
			t.Errorf("Type(%s) = %q, %v; want %q", tt.src, got, err, tt.want)
		}
	}

	// A dot-imported constant resolves like a qualified one.
	if got, err := write(t, rewrite.NewWriter(l, out), "[z]int", ctxOf(l, "example.com/rw/e")); err != nil || got != "[4]int" {
		t.Errorf("dot import: got %q, %v", got, err)
	}

	// In the package that declares them, constants are always written by name.
	if got, err := write(t, rewrite.NewWriter(l, "example.com/rw/c"), "[total]int", c); err != nil || got != "[total]int" {
		t.Errorf("own package: got %q, %v", got, err)
	}

	errs := []struct {
		src  string
		want string
	}{
		{"[yes]int", "array length is not an integer constant"},
		{"[str]int", "array length is not an integer constant"},
		{"[base / 0]int", "invalid constant expression"},
		{"[base + str]int", "array length is not an integer constant"},
		{"[base << -1]int", "invalid constant expression"},
		{"[cyc1]int", "nest too deeply"},
		{"[base + iota]int", "iota is not a constant"},
		{"[d.hidden + d.T]int", "d.T is not a constant"},
		{"[d.hidden + d.F()]int", "unsupported call in a constant expression"},
		{"[unsafe.Sizeof(base)]int", "unsupported call in a constant expression"},
		{"[<-base]int", "unsupported operator"},
		{"[base + base[0]]int", "unsupported constant expression"},
		{"[len(base)]int", "len of a non-constant string"},
		{"[int(f * 1.25)]int", "not representable"},
		{"[nope + base]int", "nope is not a constant"},
	}

	for _, tt := range errs {
		if got, err := write(t, rewrite.NewWriter(l, out), tt.src, c); err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("Type(%s) = %q, %v; want an error containing %q", tt.src, got, err, tt.want)
		}
	}
}

func TestWriter_InternalConstant(t *testing.T) {
	t.Parallel()

	l := module(t, map[string]string{
		"x/internal/p/p.go": "package p\n\nconst N = 3\n",
		"x/a/a.go":          "package a\n\nimport \"example.com/rw/x/internal/p\"\n\ntype I interface{ M([p.N]int) }\n",
	})

	c := ctxOf(l, "example.com/rw/x/a")

	for out, want := range map[string]string{
		"example.com/rw/x/b": "[p.N]int", // inside x: may import x/internal
		"example.com/rw/y":   "[3]int",   // outside x: cannot, so the value is written
	} {
		if got, err := write(t, rewrite.NewWriter(l, out), "[p.N]int", c); err != nil || got != want {
			t.Errorf("output %s: got %q, %v; want %q", out, got, err, want)
		}
	}
}

func TestWriter_Internal(t *testing.T) {
	t.Parallel()

	l := module(t, map[string]string{
		"x/internal/p/p.go": "package p\n\ntype T struct{}\n",
		"x/a/a.go":          "package a\n\nimport \"example.com/rw/x/internal/p\"\n\ntype I interface{ M(p.T) }\n",
	})
	c := ctxOf(l, "example.com/rw/x/a")

	for out, ok := range map[string]bool{
		"example.com/rw/x/b":     true, // inside x: may import x/internal
		"example.com/rw/x/b/c":   true,
		"example.com/rw/x/a":     true,
		"example.com/rw/y":       false, // outside x: cannot
		"example.com/rw/xother":  false,
		"example.com/other/x/zz": false,
		"":                       true, // unknown output package: assume it can
	} {
		got, err := write(t, rewrite.NewWriter(l, out), "p.T", c)
		if ok && (err != nil || got != "p.T") || !ok && err == nil {
			t.Errorf("output %q: got %q, %v; importable = %v", out, got, err, ok)
		}
	}
}

func TestWriter_TypeParameterArgument(t *testing.T) {
	t.Parallel()

	l := module(t, map[string]string{"model/model.go": modelSrc, "a/a.go": aSrc})
	c := ctxOf(l, "example.com/rw/a")

	// T is bound to a type written in another package's context; U is a type parameter of the
	// interface being generated.
	c.TP["T"] = canon.TypeParam{Canon: "example.com/rw/model.User", Arg: parse(t, "User"), ArgCtx: ctxOf(l, "example.com/rw/model")}
	c.TP["U"] = canon.TypeParam{Canon: "$I#0", Name: "U"}

	w := rewrite.NewWriter(l, "example.com/rw/out")

	if got, err := write(t, w, "map[U][]T", c); err != nil || got != "map[U][]model.User" {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestWriter_Constraint(t *testing.T) {
	t.Parallel()

	l := module(t, map[string]string{"model/model.go": modelSrc, "a/a.go": aSrc})
	c := ctxOf(l, "example.com/rw/model")
	w := rewrite.NewWriter(l, "example.com/rw/out")

	if got, err := write(t, w, "~int | ~string", c); err != nil || got != "~int | ~string" {
		t.Fatalf("got %q, %v", got, err)
	}

	if _, err := write(t, w, "interface{ secret }", c); err == nil || !strings.Contains(err.Error(), "unexported") {
		t.Fatalf("an unwritable constraint must be an error, got %v", err)
	}
}

func TestImports(t *testing.T) {
	t.Parallel()

	im := rewrite.NewImports()

	if got := im.Name("example.com/a/model", "model"); got != "model" {
		t.Fatalf("first use: %q", got)
	}

	if got := im.Name("example.com/b/model", "model"); got != "model2" {
		t.Fatalf("clash: %q", got)
	}

	if got := im.Name("example.com/a/model", "model"); got != "model" {
		t.Fatalf("repeat: %q", got)
	}

	if got := im.Name("example.com/c/string", "string"); got != "string2" {
		t.Fatalf("predeclared name: %q", got)
	}

	if got := im.Name("example.com/c/func", "func"); got != "func2" {
		t.Fatalf("keyword: %q", got)
	}

	if got := im.Name("example.com/sync", "sync"); got != "sync2" {
		t.Fatalf("reserved name: %q", got)
	}

	if !im.Taken("model2") || im.Taken("nothing") {
		t.Fatal("Taken is wrong")
	}

	im.Use("sync")

	lines := im.Lines()
	if !strings.Contains(lines, "\t\"sync\"\n\n\t") || !strings.Contains(lines, "\tmodel2 \"example.com/b/model\"\n") ||
		!strings.Contains(lines, "\t\"example.com/a/model\"\n") {
		t.Fatalf("unexpected import block:\n%s", lines)
	}
}
