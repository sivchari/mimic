package rewrite

import (
	"go/ast"
	"go/constant"
	"go/token"
	"strings"

	"github.com/sivchari/mimic/internal/canon"
	"github.com/sivchari/mimic/internal/load"
)

var (
	intTypes   = setOf("int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64 uintptr byte rune")
	floatTypes = setOf("float32 float64")
)

func setOf(s string) map[string]bool {
	m := map[string]bool{}
	for n := range strings.FieldsSeq(s) {
		m[n] = true
	}

	return m
}

// length writes the length expression of an array type. A length that only names constants the
// output package can reference is copied; otherwise it is evaluated from the AST and written as
// its value, so that an unexported or internal constant never has to be named.
func (w *Writer) length(e ast.Expr, c *canon.Ctx) string {
	if w.writable(e, c) {
		return w.Type(e, c)
	}

	n := constant.ToInt(w.eval(e, c, -1))
	if n.Kind() != constant.Int {
		load.Failf("%s: array length is not an integer constant", w.l.Pos(e.Pos()))
	}

	return n.ExactString()
}

// writable reports whether every identifier of the constant expression e can be written by name
// in the output package.
func (w *Writer) writable(e ast.Expr, c *canon.Ctx) bool {
	switch t := e.(type) {
	case *ast.Ident:
		if pk := c.File.Pkg; pk.Names[t.Name] {
			return w.nameable(pk.Path, t.Name)
		}

		if dp, ok := w.l.DotLookup(c.File, t.Name); ok {
			return w.nameable(dp, t.Name)
		}
	case *ast.SelectorExpr:
		if x, ok := t.X.(*ast.Ident); ok {
			if ip, ok := w.l.ImportPath(c.File, x.Name); ok {
				return w.nameable(ip, t.Sel.Name)
			}
		}
	case *ast.ParenExpr:
		return w.writable(t.X, c)
	case *ast.UnaryExpr:
		return w.writable(t.X, c)
	case *ast.BinaryExpr:
		return w.writable(t.X, c) && w.writable(t.Y, c)
	case *ast.CallExpr:
		return w.writable(t.Fun, c) && w.allWritable(t.Args, c)
	}

	return true
}

func (w *Writer) allWritable(es []ast.Expr, c *canon.Ctx) bool {
	for _, e := range es {
		if !w.writable(e, c) {
			return false
		}
	}

	return true
}

// eval evaluates the constant expression e written in context c. iota is the value of iota, or
// -1 outside a constant declaration.
func (w *Writer) eval(e ast.Expr, c *canon.Ctx, iota int) constant.Value {
	switch t := e.(type) {
	case *ast.BasicLit:
		v := constant.MakeFromLiteral(t.Value, t.Kind, 0)
		if v.Kind() == constant.Unknown {
			load.Failf("%s: invalid literal %s", w.l.Pos(t.Pos()), t.Value)
		}

		return v
	case *ast.ParenExpr:
		return w.eval(t.X, c, iota)
	case *ast.Ident:
		return w.evalIdent(t, c, iota)
	case *ast.SelectorExpr:
		return w.evalSelector(t, c)
	case *ast.UnaryExpr:
		return w.evalUnary(t, c, iota)
	case *ast.BinaryExpr:
		return w.evalBinary(t, c, iota)
	case *ast.CallExpr:
		return w.evalCall(t, c, iota)
	}

	load.Failf("%s: unsupported constant expression %T", w.l.Pos(e.Pos()), e)

	return nil
}

// op runs a go/constant operation, which panics on operands the Go spec does not allow.
func (w *Writer) op(pos token.Pos, f func() constant.Value) constant.Value {
	defer func() {
		if x := recover(); x != nil {
			load.Failf("%s: invalid constant expression: %v", w.l.Pos(pos), x)
		}
	}()

	return f()
}

func (w *Writer) evalIdent(t *ast.Ident, c *canon.Ctx, iota int) constant.Value {
	pk := c.File.Pkg

	if pk.Names[t.Name] {
		return w.evalDecl(pk, t.Name, t.Pos())
	}

	if dp, ok := w.l.DotLookup(c.File, t.Name); ok {
		return w.evalDecl(w.l.Package(dp), t.Name, t.Pos())
	}

	switch t.Name {
	case "true":
		return constant.MakeBool(true)
	case "false":
		return constant.MakeBool(false)
	case "iota":
		if iota >= 0 {
			return constant.MakeInt64(int64(iota))
		}
	}

	load.Failf("%s: %s is not a constant", w.l.Pos(t.Pos()), t.Name)

	return nil
}

func (w *Writer) evalSelector(t *ast.SelectorExpr, c *canon.Ctx) constant.Value {
	x, ok := t.X.(*ast.Ident)
	if !ok {
		load.Failf("%s: unsupported selector", w.l.Pos(t.Pos()))
	}

	ip, ok := w.l.ImportPath(c.File, x.Name)
	if !ok {
		load.Failf("%s: unknown package qualifier %q", w.l.Pos(t.Pos()), x.Name)
	}

	return w.evalDecl(w.l.Package(ip), t.Sel.Name, t.Pos())
}

// evalDecl evaluates the package-level constant name of pk in the context of its declaration.
func (w *Writer) evalDecl(pk *load.Package, name string, pos token.Pos) constant.Value {
	cd := pk.Consts[name]
	if cd == nil {
		load.Failf("%s: %s.%s is not a constant", w.l.Pos(pos), pk.Name, name)
	}

	w.enter(pos)
	defer w.leave()

	return w.eval(cd.Expr, &canon.Ctx{File: cd.File}, cd.Iota)
}

func (w *Writer) evalUnary(t *ast.UnaryExpr, c *canon.Ctx, iota int) constant.Value {
	switch t.Op {
	case token.ADD, token.SUB, token.XOR, token.NOT:
	default:
		load.Failf("%s: unsupported operator %s in a constant expression", w.l.Pos(t.Pos()), t.Op)
	}

	x := w.eval(t.X, c, iota)

	return w.op(t.Pos(), func() constant.Value { return constant.UnaryOp(t.Op, x, 0) })
}

func (w *Writer) evalBinary(t *ast.BinaryExpr, c *canon.Ctx, iota int) constant.Value {
	x, y := w.eval(t.X, c, iota), w.eval(t.Y, c, iota)

	return w.op(t.Pos(), func() constant.Value {
		switch t.Op {
		case token.EQL, token.NEQ, token.LSS, token.LEQ, token.GTR, token.GEQ:
			return constant.MakeBool(constant.Compare(x, t.Op, y))
		case token.SHL, token.SHR:
			n, ok := constant.Uint64Val(constant.ToInt(y))
			if !ok {
				panic("invalid shift count")
			}

			return constant.Shift(constant.ToInt(x), t.Op, uint(n))
		case token.QUO:
			if x.Kind() == constant.Int && y.Kind() == constant.Int {
				return constant.BinaryOp(x, token.QUO_ASSIGN, y) // integer division
			}
		}

		return constant.BinaryOp(x, t.Op, y)
	})
}

// evalCall evaluates len of a constant string and conversions of a constant.
func (w *Writer) evalCall(t *ast.CallExpr, c *canon.Ctx, iota int) constant.Value {
	if len(t.Args) != 1 {
		load.Failf("%s: unsupported call in a constant expression", w.l.Pos(t.Pos()))
	}

	fun, _ := t.Fun.(*ast.Ident)

	if fun != nil && fun.Name == "len" && !c.File.Pkg.Names["len"] {
		s := w.eval(t.Args[0], c, iota)
		if s.Kind() != constant.String {
			load.Failf("%s: len of a non-constant string in a constant expression", w.l.Pos(t.Pos()))
		}

		return constant.MakeInt64(int64(len(constant.StringVal(s))))
	}

	if !w.isType(t.Fun, c) {
		load.Failf("%s: unsupported call in a constant expression", w.l.Pos(t.Pos()))
	}

	v := w.eval(t.Args[0], c, iota)

	switch {
	case fun != nil && intTypes[fun.Name]:
		v = constant.ToInt(v)
	case fun != nil && floatTypes[fun.Name]:
		v = constant.ToFloat(v)
	}

	if v.Kind() == constant.Unknown {
		load.Failf("%s: constant conversion is not representable", w.l.Pos(t.Pos()))
	}

	return v
}

// isType reports whether the function expression of a call names a type, which makes the call a
// conversion.
func (w *Writer) isType(e ast.Expr, c *canon.Ctx) bool {
	switch t := e.(type) {
	case *ast.Ident:
		if pk := c.File.Pkg; pk.Names[t.Name] {
			return pk.Types[t.Name] != nil
		}

		if dp, ok := w.l.DotLookup(c.File, t.Name); ok {
			return w.l.Package(dp).Types[t.Name] != nil
		}

		return canon.IsPredeclared(t.Name)
	case *ast.SelectorExpr:
		x, ok := t.X.(*ast.Ident)
		if !ok {
			return false
		}

		ip, ok := w.l.ImportPath(c.File, x.Name)

		return ok && ip != "unsafe"
	case *ast.ParenExpr:
		return w.isType(t.X, c)
	}

	return false
}
