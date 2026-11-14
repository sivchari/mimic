// Package canon computes canonical, import-path based identities of type expressions and defines
// the resolution context shared by the packages that read them.
//
// Identities tell whether two spellings denote the same type, for example to accept a method
// that is inherited twice. They do not depend on how a package is imported (alias, dot import)
// or on formatting.
package canon

import (
	"go/ast"
	"sort"
	"strings"

	"github.com/sivchari/mimic/internal/load"
)

// TypeParam describes what a type parameter name means inside one declaration.
type TypeParam struct {
	// Canon is the canonical identity.
	Canon string
	// Name is the text to write for a type parameter of the interface being generated.
	Name string
	// Arg is the type argument substituted for a type parameter of an embedded generic
	// interface; it is written as an expression of ArgCtx. Nil for Name.
	Arg    ast.Expr
	ArgCtx *Ctx
}

// Ctx is the resolution context of a type expression: the file it was written in and the type
// parameters visible there.
type Ctx struct {
	File *load.File
	TP   map[string]TypeParam
}

var universe = map[string]bool{}

var constBuiltins = map[string]bool{}

func init() {
	for n := range strings.FieldsSeq("bool byte complex64 complex128 error float32 float64 int int8 int16 int32 int64 rune string uint uint8 uint16 uint32 uint64 uintptr any comparable") {
		universe[n] = true
	}

	for n := range strings.FieldsSeq("len cap true false iota nil min max real imag complex") {
		constBuiltins[n] = true
	}
}

// IsPredeclared reports whether name is a predeclared type identifier.
func IsPredeclared(name string) bool { return universe[name] }

// IsConstBuiltin reports whether name is a predeclared identifier that may appear in a constant
// expression such as an array length.
func IsConstBuiltin(name string) bool { return constBuiltins[name] }

// Expr returns the canonical identity of the type expression e written in context c.
func Expr(l *load.Loader, e ast.Expr, c *Ctx) string {
	switch t := e.(type) {
	case *ast.Ident:
		return ident(l, t, c)
	case *ast.SelectorExpr:
		return selector(l, t, c)
	case *ast.StarExpr:
		return "*" + Expr(l, t.X, c)
	case *ast.ParenExpr:
		return Expr(l, t.X, c)
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + Expr(l, t.Elt, c)
		}

		return "[" + Expr(l, t.Len, c) + "]" + Expr(l, t.Elt, c)
	case *ast.MapType:
		return "map[" + Expr(l, t.Key, c) + "]" + Expr(l, t.Value, c)
	case *ast.ChanType:
		return chanType(l, t, c)
	case *ast.FuncType:
		return "func" + sig(l, t, c)
	case *ast.InterfaceType:
		return interfaceType(l, t, c)
	case *ast.StructType:
		return structType(l, t, c)
	case *ast.IndexExpr:
		return Expr(l, t.X, c) + "[" + Expr(l, t.Index, c) + "]"
	case *ast.IndexListExpr:
		return Expr(l, t.X, c) + "[" + join(l, t.Indices, c, ",") + "]"
	case *ast.BasicLit:
		return t.Value
	case *ast.BinaryExpr:
		return "(" + Expr(l, t.X, c) + t.Op.String() + Expr(l, t.Y, c) + ")"
	case *ast.UnaryExpr:
		return t.Op.String() + Expr(l, t.X, c)
	case *ast.CallExpr:
		return Expr(l, t.Fun, c) + "(" + join(l, t.Args, c, ",") + ")"
	case *ast.Ellipsis:
		return "..." + Expr(l, t.Elt, c)
	}

	load.Failf("%s: unsupported type expression %T", l.Pos(e.Pos()), e)

	return ""
}

func ident(l *load.Loader, t *ast.Ident, c *Ctx) string {
	if v, ok := c.TP[t.Name]; ok {
		return v.Canon
	}

	if pk := c.File.Pkg; pk.Names[t.Name] {
		return pk.Path + "." + t.Name
	}

	if dp, ok := l.DotLookup(c.File, t.Name); ok {
		return dp + "." + t.Name
	}

	if universe[t.Name] || constBuiltins[t.Name] {
		return t.Name
	}

	load.Failf("%s: unresolved identifier %q", l.Pos(t.Pos()), t.Name)

	return ""
}

func selector(l *load.Loader, t *ast.SelectorExpr, c *Ctx) string {
	x, ok := t.X.(*ast.Ident)
	if !ok {
		load.Failf("%s: unsupported selector", l.Pos(t.Pos()))
	}

	ip, ok := l.ImportPath(c.File, x.Name)
	if !ok {
		load.Failf("%s: unknown package qualifier %q", l.Pos(t.Pos()), x.Name)
	}

	return ip + "." + t.Sel.Name
}

func chanType(l *load.Loader, t *ast.ChanType, c *Ctx) string {
	el := "(" + Expr(l, t.Value, c) + ")"

	switch t.Dir {
	case ast.SEND:
		return "chan<-" + el
	case ast.RECV:
		return "<-chan" + el
	}

	return "chan" + el
}

func interfaceType(l *load.Loader, t *ast.InterfaceType, c *Ctx) string {
	var parts []string

	for _, f := range t.Methods.List {
		if ft, ok := f.Type.(*ast.FuncType); ok && len(f.Names) > 0 {
			parts = append(parts, f.Names[0].Name+sig(l, ft, c))
		} else {
			parts = append(parts, Expr(l, f.Type, c))
		}
	}

	sort.Strings(parts)

	return "interface{" + strings.Join(parts, ";") + "}"
}

func structType(l *load.Loader, t *ast.StructType, c *Ctx) string {
	var parts []string

	for _, f := range t.Fields.List {
		tag := ""
		if f.Tag != nil {
			tag = " " + f.Tag.Value
		}

		ts := Expr(l, f.Type, c)
		if len(f.Names) == 0 {
			parts = append(parts, ts+tag)
		}

		for _, n := range f.Names {
			parts = append(parts, n.Name+" "+ts+tag)
		}
	}

	return "struct{" + strings.Join(parts, ";") + "}"
}

func join(l *load.Loader, es []ast.Expr, c *Ctx, sep string) string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = Expr(l, e, c)
	}

	return strings.Join(parts, sep)
}

// sig returns the canonical identity of a function signature: parameter and result types without
// names.
func sig(l *load.Loader, ft *ast.FuncType, c *Ctx) string {
	list := func(fl *ast.FieldList) []string {
		if fl == nil {
			return nil
		}

		var out []string

		for _, f := range fl.List {
			ts := Expr(l, f.Type, c)
			for range max(1, len(f.Names)) {
				out = append(out, ts)
			}
		}

		return out
	}

	s := "(" + strings.Join(list(ft.Params), ",") + ")"
	if r := list(ft.Results); len(r) > 0 {
		s += "(" + strings.Join(r, ",") + ")"
	}

	return s
}
