package canon_test

import (
	"go/parser"
	"strings"
	"testing"

	"github.com/sivchari/mimic/internal/canon"
	"github.com/sivchari/mimic/internal/load"
)

func testCtx(t *testing.T) (*load.Loader, *canon.Ctx) {
	t.Helper()

	l, err := load.New("../../testdata/suite")
	if err != nil {
		t.Fatal(err)
	}

	// Parse a real file so that import resolution works: tricky/a.go imports model as m.
	var file *load.File

	for _, f := range l.Package("example.com/suite/tricky").Files {
		if strings.HasSuffix(l.Fset.Position(f.AST.Pos()).Filename, "a.go") {
			file = f
		}
	}

	return l, &canon.Ctx{File: file, TP: map[string]canon.TypeParam{"K": {Canon: "$K#0", Name: "K"}}}
}

func TestExpr(t *testing.T) {
	t.Parallel()

	l, c := testCtx(t)

	tests := []struct{ src, want string }{
		{"int", "int"},
		{"User", "example.com/suite/tricky.User"},
		{"*User", "*example.com/suite/tricky.User"},
		{"(User)", "example.com/suite/tricky.User"},
		{"m.User", "example.com/suite/model.User"},
		{"[]map[string]*m.User", "[]map[string]*example.com/suite/model.User"},
		{"[4]byte", "[4]byte"},
		{"[len(\"ab\")]byte", "[len(\"ab\")]byte"},
		{"chan int", "chan(int)"},
		{"chan<- int", "chan<-(int)"},
		{"<-chan int", "<-chan(int)"},
		{"func(a, b int, c ...string) (x error, y bool)", "func(int,int,...string)(error,bool)"},
		{"func()", "func()"},
		{"struct{ A int `json:\"a\"`; B, C string }", "struct{A int `json:\"a\"`;B string;C string}"},
		{"interface{ Z(int); A() }", "interface{A();Z(int)}"},
		{"K", "$K#0"},
		{"User[int]", "example.com/suite/tricky.User[int]"},
		{"User[int, string]", "example.com/suite/tricky.User[int,string]"},
		{"[1 + 2]int", "[(1+2)]int"},
	}

	for _, tt := range tests {
		e, err := parser.ParseExpr(tt.src)
		if err != nil {
			t.Fatalf("%s: %v", tt.src, err)
		}

		if got := canon.Expr(l, e, c); got != tt.want {
			t.Errorf("Expr(%s) = %q, want %q", tt.src, got, tt.want)
		}
	}
}

func TestExpr_Errors(t *testing.T) {
	t.Parallel()

	l, c := testCtx(t)

	tests := []struct{ src, want string }{
		{"Nope", `unresolved identifier "Nope"`},
		{"zz.Thing", `unknown package qualifier "zz"`},
		{"a.b.C", "unsupported selector"},
		{"x[0:1]", "unsupported type expression"},
	}

	for _, tt := range tests {
		e, err := parser.ParseExpr(tt.src)
		if err != nil {
			t.Fatalf("%s: %v", tt.src, err)
		}

		var got error

		func() {
			defer load.Recover(&got)

			canon.Expr(l, e, c)
		}()

		if got == nil || !strings.Contains(got.Error(), tt.want) {
			t.Errorf("Expr(%s): got %v, want %q", tt.src, got, tt.want)
		}
	}
}

func TestPredeclared(t *testing.T) {
	t.Parallel()

	if !canon.IsPredeclared("string") || canon.IsPredeclared("len") || !canon.IsConstBuiltin("len") || canon.IsConstBuiltin("string") {
		t.Fatal("predeclared tables are wrong")
	}
}
