// Package rewrite writes the source text of a type expression into another package.
//
// Only what changes meaning when a type moves is rewritten: identifier qualification, import
// names, and the references that the output package cannot spell. Unexported aliases and
// aliases of internal packages are replaced by their targets, and array lengths that name an
// unreachable constant are replaced by the constant's value. Anything else that cannot be
// spelled from the output package is an error that names the package to generate in.
package rewrite

import (
	"go/ast"
	"go/token"
	"slices"
	"strings"

	"github.com/sivchari/mimic/internal/canon"
	"github.com/sivchari/mimic/internal/load"
)

// maxDepth bounds the nesting of alias and constant expansion, which only a cyclic (invalid)
// declaration can exceed.
const maxDepth = 64

// Writer rewrites types for one output package. It keeps the import table of the output file.
type Writer struct {
	Imports *Imports

	l       *load.Loader
	outPath string // import path of the output package ("" if unknown)
	depth   int
	refs    *[]string // import names of the type being written, while TypeRefs runs
}

// NewWriter returns a Writer for an output package with import path outPath ("" if unknown).
func NewWriter(l *load.Loader, outPath string) *Writer {
	im := NewImports()
	im.pkgName = l.PackageName

	return &Writer{Imports: im, l: l, outPath: outPath}
}

// IsSelf reports whether p is the output package.
func (w *Writer) IsSelf(p string) bool { return w.outPath != "" && p == w.outPath }

// Qualify returns the qualifier ("pkg.") for a reference to package p, or "" for the output
// package itself.
func (w *Writer) Qualify(p, pref string) string {
	if w.IsSelf(p) {
		return ""
	}

	n := w.Imports.Name(p, pref)
	if w.refs != nil {
		*w.refs = append(*w.refs, n)
	}

	return n + "."
}

// findInternal returns the parent of the last "internal" element of the import path p.
func findInternal(p string) (parent string, ok bool) {
	parts := strings.Split(p, "/")

	for i, part := range slices.Backward(parts) {
		if part == "internal" {
			return strings.Join(parts[:i], "/"), true
		}
	}

	return "", false
}

// internalOK reports whether the output package may import p (internal/ rule).
func (w *Writer) internalOK(p string) bool {
	parent, ok := findInternal(p)
	if !ok || w.outPath == "" {
		return true
	}

	if parent == "" {
		return false // standard library internal
	}

	return w.outPath == parent || strings.HasPrefix(w.outPath, parent+"/")
}

// nameable reports whether the identifier name of package path can be written by name in the
// output package.
func (w *Writer) nameable(path, name string) bool {
	return w.IsSelf(path) || ast.IsExported(name) && w.internalOK(path)
}

// refuse aborts with a message that names where the identifier could be used.
func (w *Writer) refuse(pk *load.Package, name string) string {
	if !ast.IsExported(name) {
		load.Failf("%s.%s is unexported; generate the mock in package %s", pk.Name, name, pk.Name)
	}

	parent, _ := findInternal(pk.Path)
	if parent == "" {
		load.Failf("%s is internal to the standard library", pk.Path)
	}

	load.Failf("%s is internal; generate the mock inside %s", pk.Path, parent)

	return ""
}

// enter guards the expansion of aliases and constants against cyclic declarations.
func (w *Writer) enter(pos token.Pos) {
	if w.depth++; w.depth > maxDepth {
		load.Failf("%s: declarations nest too deeply (cycle?)", w.l.Pos(pos))
	}
}

func (w *Writer) leave() { w.depth-- }

// TypeRefs writes the type expression e like Type and also returns the file-level names of the
// imports the text refers to, in order of appearance.
func (w *Writer) TypeRefs(e ast.Expr, c *canon.Ctx) (text string, imports []string) {
	w.refs = &imports

	defer func() { w.refs = nil }()

	return w.Type(e, c), imports
}

// Type writes the type expression e as seen from the output package.
func (w *Writer) Type(e ast.Expr, c *canon.Ctx) string {
	switch t := e.(type) {
	case *ast.Ident:
		return w.ident(t, nil, c)
	case *ast.SelectorExpr:
		return w.selector(t, nil, c)
	case *ast.StarExpr:
		return "*" + w.Type(t.X, c)
	case *ast.ParenExpr:
		return "(" + w.Type(t.X, c) + ")"
	case *ast.ArrayType:
		if t.Len == nil {
			return "[]" + w.Type(t.Elt, c)
		}

		return "[" + w.length(t.Len, c) + "]" + w.Type(t.Elt, c)
	case *ast.MapType:
		return "map[" + w.Type(t.Key, c) + "]" + w.Type(t.Value, c)
	case *ast.ChanType:
		return w.chanType(t, c)
	case *ast.FuncType:
		return "func" + w.sig(t, c)
	case *ast.InterfaceType:
		return w.interfaceType(t, c)
	case *ast.StructType:
		return w.structType(t, c)
	case *ast.IndexExpr:
		return w.instance(t.X, []ast.Expr{t.Index}, c)
	case *ast.IndexListExpr:
		return w.instance(t.X, t.Indices, c)
	case *ast.BasicLit:
		return t.Value
	case *ast.BinaryExpr:
		return w.Type(t.X, c) + " " + t.Op.String() + " " + w.Type(t.Y, c)
	case *ast.UnaryExpr:
		return t.Op.String() + w.Type(t.X, c)
	case *ast.CallExpr:
		return w.Type(t.Fun, c) + "(" + w.list(t.Args, c) + ")"
	case *ast.Ellipsis:
		return "..." + w.Type(t.Elt, c)
	}

	load.Failf("%s: unsupported type expression %T", w.l.Pos(e.Pos()), e)

	return ""
}

func (w *Writer) ident(t *ast.Ident, args []ast.Expr, c *canon.Ctx) string {
	if v, ok := c.TP[t.Name]; ok {
		if v.Arg != nil {
			return w.Type(v.Arg, v.ArgCtx)
		}

		return v.Name
	}

	if pk := c.File.Pkg; pk.Names[t.Name] {
		if w.nameable(pk.Path, t.Name) {
			return w.name(pk.Path, pk.Name, t.Name, args, c)
		}

		return w.expand(pk, t.Name, args, c)
	}

	if dp, ok := w.l.DotLookup(c.File, t.Name); ok {
		pk := w.l.Package(dp)
		if w.nameable(dp, t.Name) {
			return w.name(dp, pk.Name, t.Name, args, c)
		}

		return w.expand(pk, t.Name, args, c)
	}

	if canon.IsPredeclared(t.Name) || canon.IsConstBuiltin(t.Name) {
		return t.Name
	}

	load.Failf("%s: unresolved identifier %q", w.l.Pos(t.Pos()), t.Name)

	return ""
}

func (w *Writer) selector(t *ast.SelectorExpr, args []ast.Expr, c *canon.Ctx) string {
	x, ok := t.X.(*ast.Ident)
	if !ok {
		load.Failf("%s: unsupported selector", w.l.Pos(t.Pos()))
	}

	ip, ok := w.l.ImportPath(c.File, x.Name)
	if !ok {
		load.Failf("%s: unknown package qualifier %q", w.l.Pos(t.Pos()), x.Name)
	}

	// The package is only parsed when the name cannot be used as it is.
	if w.nameable(ip, t.Sel.Name) {
		return w.name(ip, x.Name, t.Sel.Name, args, c)
	}

	return w.expand(w.l.Package(ip), t.Sel.Name, args, c)
}

// instance writes a generic type instantiated with args.
func (w *Writer) instance(x ast.Expr, args []ast.Expr, c *canon.Ctx) string {
	switch t := x.(type) {
	case *ast.Ident:
		return w.ident(t, args, c)
	case *ast.SelectorExpr:
		return w.selector(t, args, c)
	}

	load.Failf("%s: unsupported type instantiation", w.l.Pos(x.Pos()))

	return ""
}

// name writes a reference to the identifier name of package path by name.
func (w *Writer) name(path, pref, name string, args []ast.Expr, c *canon.Ctx) string {
	s := w.Qualify(path, pref) + name
	if len(args) > 0 {
		s += "[" + w.list(args, c) + "]"
	}

	return s
}

// expand writes a reference the output package cannot spell by name. An alias is replaced by its
// target, written in the declaring package's context; anything else is refused.
func (w *Writer) expand(pk *load.Package, name string, args []ast.Expr, c *canon.Ctx) string {
	td := pk.Types[name]
	if td == nil || !td.IsAlias() {
		return w.refuse(pk, name)
	}

	ac := &canon.Ctx{File: td.File, TP: map[string]canon.TypeParam{}}

	if td.Spec.TypeParams != nil {
		i := 0

		for _, f := range td.Spec.TypeParams.List {
			for _, n := range f.Names {
				if i >= len(args) {
					load.Failf("%s: missing type argument for %s.%s", w.l.Pos(td.Spec.Pos()), pk.Name, name)
				}

				ac.TP[n.Name] = canon.TypeParam{Name: n.Name, Arg: args[i], ArgCtx: c}
				i++
			}
		}
	}

	w.enter(td.Spec.Pos())
	defer w.leave()

	return w.Type(td.Spec.Type, ac)
}

func (w *Writer) list(es []ast.Expr, c *canon.Ctx) string {
	parts := make([]string, len(es))
	for i, e := range es {
		parts[i] = w.Type(e, c)
	}

	return strings.Join(parts, ", ")
}

func (w *Writer) chanType(t *ast.ChanType, c *canon.Ctx) string {
	v := w.Type(t.Value, c)
	if strings.HasPrefix(v, "chan ") || strings.HasPrefix(v, "chan<- ") || strings.HasPrefix(v, "<-chan ") {
		v = "(" + v + ")"
	}

	switch t.Dir {
	case ast.SEND:
		return "chan<- " + v
	case ast.RECV:
		return "<-chan " + v
	}

	return "chan " + v
}

func (w *Writer) interfaceType(t *ast.InterfaceType, c *canon.Ctx) string {
	parts := make([]string, 0, len(t.Methods.List))

	for _, f := range t.Methods.List {
		ft, isMethod := f.Type.(*ast.FuncType)
		if !isMethod || len(f.Names) == 0 {
			parts = append(parts, w.Type(f.Type, c))

			continue
		}

		if pk := c.File.Pkg; !ast.IsExported(f.Names[0].Name) && !w.IsSelf(pk.Path) {
			load.Failf("interface literal has unexported method %s; generate the mock in package %s", f.Names[0].Name, pk.Name)
		}

		parts = append(parts, f.Names[0].Name+w.sig(ft, c))
	}

	return "interface{" + strings.Join(parts, "; ") + "}"
}

func (w *Writer) structType(t *ast.StructType, c *canon.Ctx) string {
	pk := c.File.Pkg
	parts := make([]string, 0, len(t.Fields.List))

	for _, f := range t.Fields.List {
		names := make([]string, len(f.Names))

		for i, n := range f.Names {
			if !ast.IsExported(n.Name) && !w.IsSelf(pk.Path) {
				load.Failf("struct literal has unexported field %s; generate the mock in package %s", n.Name, pk.Name)
			}

			names[i] = n.Name
		}

		if len(names) == 0 && !ast.IsExported(embeddedName(f.Type)) && !w.IsSelf(pk.Path) {
			load.Failf("struct literal has unexported embedded field %s; generate the mock in package %s", embeddedName(f.Type), pk.Name)
		}

		tag := ""
		if f.Tag != nil {
			tag = " " + f.Tag.Value
		}

		ft := w.Type(f.Type, c)

		if len(names) > 0 {
			parts = append(parts, strings.Join(names, ", ")+" "+ft+tag)

			continue
		}

		parts = append(parts, ft+tag)
	}

	return "struct{" + strings.Join(parts, "; ") + "}"
}

// embeddedName returns the field name of an embedded field of type e.
func embeddedName(e ast.Expr) string {
	switch t := e.(type) {
	case *ast.StarExpr:
		return embeddedName(t.X)
	case *ast.IndexExpr:
		return embeddedName(t.X)
	case *ast.IndexListExpr:
		return embeddedName(t.X)
	case *ast.SelectorExpr:
		return t.Sel.Name
	case *ast.Ident:
		return t.Name
	}

	return ""
}

// sig writes "(params) results" of a function type, keeping parameter names.
func (w *Writer) sig(ft *ast.FuncType, c *canon.Ctx) string {
	s := "(" + strings.Join(w.fields(ft.Params, c), ", ") + ")"

	if ft.Results == nil || len(ft.Results.List) == 0 {
		return s
	}

	r := w.fields(ft.Results, c)
	if len(r) == 1 && len(ft.Results.List[0].Names) == 0 {
		return s + " " + r[0]
	}

	return s + " (" + strings.Join(r, ", ") + ")"
}

func (w *Writer) fields(fl *ast.FieldList, c *canon.Ctx) []string {
	if fl == nil {
		return nil
	}

	out := make([]string, 0, len(fl.List))

	for _, f := range fl.List {
		ts := w.Type(f.Type, c)
		if len(f.Names) == 0 {
			out = append(out, ts)

			continue
		}

		names := make([]string, len(f.Names))
		for i, n := range f.Names {
			names[i] = n.Name
		}

		out = append(out, strings.Join(names, ", ")+" "+ts)
	}

	return out
}
