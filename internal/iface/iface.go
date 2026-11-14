// Package iface reads an interface declaration from syntax: its flattened method set (embedded
// interfaces and generic instantiations resolved) with, for each type, its canonical identity.
package iface

import (
	"fmt"
	"go/ast"
	"sort"
	"strings"

	"github.com/sivchari/mimic/internal/canon"
	"github.com/sivchari/mimic/internal/load"
)

// Param is one parameter of a method.
type Param struct {
	Name  string // source name; "" if the parameter is unnamed
	Canon string
	// Out is the type text in the generated file. It is filled in by the emitter.
	Out string
	// Var and Field are the variable in the generated method and the field of the call record.
	// They are filled in by the emitter.
	Var, Field string
	Expr       ast.Expr // element type (variadic: without the ellipsis)
	Ctx        *canon.Ctx
}

// Result is one result of a method.
type Result struct {
	Name  string // source name; "" if the result is unnamed
	Canon string
	// Out is the type text in the generated file. It is filled in by the emitter.
	Out  string
	Expr ast.Expr // nil for the string result of an embedded error
	Ctx  *canon.Ctx
}

// Method is one method of the flattened method set.
type Method struct {
	Name     string
	Params   []Param
	Variadic bool
	Results  []Result
	Pkg      string // import path of the package that declares the method
}

// Exported reports whether the method name is exported.
func (m *Method) Exported() bool { return ast.IsExported(m.Name) }

// Sig returns the canonical signature including parameter names and the variadic marker. Two
// methods of the same name must have equal signatures to be merged. Unnamed and blank
// parameters are named by position, so they compare equal to each other.
func (m *Method) Sig() string {
	ps := make([]string, len(m.Params))

	for i, p := range m.Params {
		k := p.Canon
		if m.Variadic && i == len(m.Params)-1 {
			k = "..." + k
		}

		n := p.Name
		if n == "" || n == "_" {
			n = fmt.Sprintf("a%d", i)
		}

		ps[i] = n + " " + k
	}

	rs := make([]string, len(m.Results))
	for i, r := range m.Results {
		rs[i] = r.Canon
	}

	return m.Name + "(" + strings.Join(ps, ", ") + ")(" + strings.Join(rs, ", ") + ")"
}

// TParam is a type parameter of the interface.
type TParam struct {
	Name       string
	Constraint ast.Expr
}

// Interface is an interface declaration with its methods flattened and sorted by name.
type Interface struct {
	Path    string // import path of the declaring package
	PkgName string
	Name    string
	TParams []TParam
	Methods []*Method
	// Ctx is the context in which the constraints of TParams are written.
	Ctx *canon.Ctx
}

// Load reads the interface name declared in the package with import path pkgpath.
func Load(l *load.Loader, pkgpath, name string) (it *Interface, err error) {
	defer load.Recover(&err)

	pi := l.Package(pkgpath)

	td := pi.Types[name]
	if td == nil {
		load.Failf("%s.%s not found", pkgpath, name)
	}

	if !mayBeInterface(td.Spec.Type, pi) {
		load.Failf("%s.%s is not an interface", pi.Name, name)
	}

	it = &Interface{Path: pkgpath, PkgName: pi.Name, Name: name}
	id := pkgpath + "." + name
	c := &canon.Ctx{File: td.File, TP: map[string]canon.TypeParam{}}
	it.Ctx = c

	if td.Spec.TypeParams != nil {
		idx := 0

		for _, f := range td.Spec.TypeParams.List {
			for _, n := range f.Names {
				c.TP[n.Name] = canon.TypeParam{Canon: fmt.Sprintf("$%s#%d", id, idx), Name: n.Name}
				it.TParams = append(it.TParams, TParam{Name: n.Name, Constraint: f.Type})
				idx++
			}
		}
	}

	cl := &collector{l: l, out: map[string]*Method{}, seen: map[string]bool{id: true}}
	cl.expr(td.Spec.Type, c)

	for _, m := range cl.out {
		it.Methods = append(it.Methods, m)
	}

	sort.Slice(it.Methods, func(i, j int) bool { return it.Methods[i].Name < it.Methods[j].Name })

	if len(it.Methods) == 0 {
		load.Failf("%s has no methods", id)
	}

	return it, nil
}

// mayBeInterface reports whether the underlying type expression e of a declaration in pk can
// denote an interface. Names and instantiations are resolved later.
func mayBeInterface(e ast.Expr, pk *load.Package) bool {
	switch t := e.(type) {
	case *ast.ParenExpr:
		return mayBeInterface(t.X, pk)
	case *ast.InterfaceType, *ast.SelectorExpr, *ast.IndexExpr, *ast.IndexListExpr:
		return true
	case *ast.Ident:
		return pk.Names[t.Name] || t.Name == "error" || t.Name == "any" || t.Name == "comparable" || !canon.IsPredeclared(t.Name)
	}

	return false
}

func buildMethod(l *load.Loader, name string, ft *ast.FuncType, c *canon.Ctx) *Method {
	m := &Method{Name: name, Pkg: c.File.Pkg.Path}

	for _, f := range ft.Params.List {
		typ := f.Type
		if el, ok := typ.(*ast.Ellipsis); ok {
			typ = el.Elt
			m.Variadic = true
		}

		key := canon.Expr(l, typ, c)

		for i := range max(1, len(f.Names)) {
			nm := ""
			if len(f.Names) > 0 {
				nm = f.Names[i].Name
			}

			m.Params = append(m.Params, Param{Name: nm, Canon: key, Expr: typ, Ctx: c})
		}
	}

	if ft.Results != nil {
		for _, f := range ft.Results.List {
			key := canon.Expr(l, f.Type, c)
			for i := range max(1, len(f.Names)) {
				nm := ""
				if len(f.Names) > 0 {
					nm = f.Names[i].Name
				}

				m.Results = append(m.Results, Result{Name: nm, Canon: key, Expr: f.Type, Ctx: c})
			}
		}
	}

	return m
}

type collector struct {
	l    *load.Loader
	out  map[string]*Method
	seen map[string]bool
}

// collect adds the methods of the interface type name declared in pkgpath, instantiated with
// args (written in context caller).
func (cl *collector) collect(pkgpath, name string, args []ast.Expr, caller *canon.Ctx) {
	pk := cl.l.Package(pkgpath)

	td := pk.Types[name]
	if td == nil {
		load.Failf("%s.%s not found", pkgpath, name)
	}

	if !mayBeInterface(td.Spec.Type, pk) {
		load.Failf("%s.%s is not an interface", pk.Name, name)
	}

	id := pkgpath + "." + name

	if len(args) > 0 {
		ks := make([]string, len(args))
		for i, a := range args {
			ks[i] = canon.Expr(cl.l, a, caller)
		}

		id += "[" + strings.Join(ks, ",") + "]"
	}

	if cl.seen[id] {
		return
	}

	cl.seen[id] = true

	c := &canon.Ctx{File: td.File, TP: map[string]canon.TypeParam{}}

	if td.Spec.TypeParams != nil {
		idx := 0

		for _, f := range td.Spec.TypeParams.List {
			for _, n := range f.Names {
				if idx >= len(args) {
					load.Failf("%s: missing type argument for %s", cl.l.Pos(td.Spec.Pos()), id)
				}

				c.TP[n.Name] = canon.TypeParam{Canon: canon.Expr(cl.l, args[idx], caller), Arg: args[idx], ArgCtx: caller}
				idx++
			}
		}
	}

	cl.expr(td.Spec.Type, c)
}

func (cl *collector) expr(e ast.Expr, c *canon.Ctx) {
	switch t := e.(type) {
	case *ast.InterfaceType:
		cl.interfaceType(t, c)
	case *ast.ParenExpr:
		cl.expr(t.X, c)
	case *ast.Ident:
		cl.ident(t, c)
	case *ast.SelectorExpr:
		x, _ := t.X.(*ast.Ident)
		if x == nil {
			load.Failf("%s: unsupported embedded selector", cl.l.Pos(t.Pos()))
		}

		ip, ok := cl.l.ImportPath(c.File, x.Name)
		if !ok {
			load.Failf("%s: unknown package qualifier %q", cl.l.Pos(t.Pos()), x.Name)
		}

		cl.collect(ip, t.Sel.Name, nil, c)
	case *ast.IndexExpr:
		cl.embedGeneric(t.X, []ast.Expr{t.Index}, c)
	case *ast.IndexListExpr:
		cl.embedGeneric(t.X, t.Indices, c)
	default:
		load.Failf("%s: unsupported embedded or underlying expression %T", cl.l.Pos(e.Pos()), e)
	}
}

func (cl *collector) interfaceType(t *ast.InterfaceType, c *canon.Ctx) {
	for _, f := range t.Methods.List {
		ft, isMethod := f.Type.(*ast.FuncType)
		if !isMethod || len(f.Names) == 0 {
			cl.expr(f.Type, c)

			continue
		}

		m := buildMethod(cl.l, f.Names[0].Name, ft, c)
		if old, dup := cl.out[m.Name]; dup {
			if old.Sig() != m.Sig() {
				load.Failf("%s: method %s declared twice with different signatures", cl.l.Pos(f.Pos()), m.Name)
			}

			continue
		}

		cl.out[m.Name] = m
	}
}

func (cl *collector) ident(t *ast.Ident, c *canon.Ctx) {
	pk := c.File.Pkg

	switch {
	case pk.Types[t.Name] != nil:
		cl.collect(pk.Path, t.Name, nil, c)
	case t.Name == "error" && !pk.Names["error"]:
		if _, dup := cl.out["Error"]; !dup {
			cl.out["Error"] = &Method{
				Name:    "Error",
				Results: []Result{{Canon: "string", Ctx: c}},
				Pkg:     pk.Path,
			}
		}
	case t.Name == "any" || t.Name == "comparable":
	default:
		if dp, ok := cl.l.DotLookup(c.File, t.Name); ok {
			cl.collect(dp, t.Name, nil, c)

			return
		}

		if canon.IsPredeclared(t.Name) {
			load.Failf("%s: %s is not an interface", cl.l.Pos(t.Pos()), t.Name)
		}

		load.Failf("%s: cannot resolve embedded %q", cl.l.Pos(t.Pos()), t.Name)
	}
}

func (cl *collector) embedGeneric(x ast.Expr, args []ast.Expr, c *canon.Ctx) {
	switch t := x.(type) {
	case *ast.Ident:
		if c.File.Pkg.Types[t.Name] != nil {
			cl.collect(c.File.Pkg.Path, t.Name, args, c)

			return
		}

		if dp, ok := cl.l.DotLookup(c.File, t.Name); ok {
			cl.collect(dp, t.Name, args, c)

			return
		}
	case *ast.SelectorExpr:
		if xi, _ := t.X.(*ast.Ident); xi != nil {
			if ip, ok := cl.l.ImportPath(c.File, xi.Name); ok {
				cl.collect(ip, t.Sel.Name, args, c)

				return
			}
		}
	}

	load.Failf("%s: cannot resolve embedded generic interface", cl.l.Pos(x.Pos()))
}
