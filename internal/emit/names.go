package emit

import (
	"go/ast"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/sivchari/mimic/internal/canon"
	"github.com/sivchari/mimic/internal/iface"
	"github.com/sivchari/mimic/internal/load"
	"github.com/sivchari/mimic/internal/rewrite"
)

// The naming of parameters follows moq v0.6.0 (internal/registry and internal/template) so that
// the fields of a call record are the same as in a moq mock. moq derives names from go/types; this
// file derives them from the syntax of the type, which gives the same names except where a name
// depends on a type that only the type checker can see (see typeName).

// moqReserved are the derived names that moq gives the suffix MoqParam: names the generated
// code uses, keywords, and predeclared types.
var moqReserved = map[string]bool{}

// initialisms are the names that moq's Exported upper-cases as a whole (golint's list).
var initialisms = []string{
	"ACL", "API", "ASCII", "CPU", "CSS", "DNS", "EOF", "GUID", "HTML", "HTTP", "HTTPS", "ID", "IP", "JSON", "LHS",
	"QPS", "RAM", "RHS", "RPC", "SLA", "SMTP", "SQL", "SSH", "TCP", "TLS", "TTL", "UDP", "UI", "UID", "UUID", "URI",
	"URL", "UTF8", "VM", "XML", "XMPP", "XSRF", "XSS",
}

func init() {
	for n := range strings.FieldsSeq(`mock callInfo break default func interface select case defer go map struct
		chan else goto package switch const fallthrough if range type continue for import return var
		string bool byte rune uintptr int int8 int16 int32 int64 uint uint8 uint16 uint32 uint64
		float32 float64 complex64 complex128`) {
		moqReserved[n] = true
	}
}

// namer chooses the identifiers of the parameters of the methods of one output file.
type namer struct {
	l       *load.Loader
	w       *rewrite.Writer
	aliases map[string]bool // "path.Name" -> declared as an alias
}

func newNamer(l *load.Loader, w *rewrite.Writer) *namer {
	return &namer{l: l, w: w, aliases: map[string]bool{}}
}

// variable is a name allocated in a method scope. A later allocation can rename it.
type variable struct{ name string }

// scope allocates the names of the parameters and results of one method like moq's MethodScope:
// a name that equals an import name or an earlier name gets a suffix, and an earlier name can be
// renamed when a later type imports a package of that name.
type scope struct {
	imports    *rewrite.Imports
	vars       []*variable
	conflicted map[string]bool
}

func (s *scope) search(name string) *variable {
	for _, v := range s.vars {
		if v.name == name {
			return v
		}
	}

	return nil
}

// add allocates a variable named name whose type refers to the imports refs.
func (s *scope) add(refs []string, name string) *variable {
	for _, r := range refs {
		if v := s.search(r); v != nil {
			v.name += "MoqParam"
		}
	}

	if s.imports.Registered(name) {
		name += "MoqParam"
	}

	if s.search(name) != nil || s.conflicted[name] {
		name = s.resolve(name)
	}

	v := &variable{name: name}
	s.vars = append(s.vars, v)

	return v
}

// resolve numbers a name that is already taken: the first holder becomes name1 and the newcomer
// name2 (or the next free number).
func (s *scope) resolve(name string) string {
	for n := 1; ; n++ {
		if s.search(name+strconv.Itoa(n)) != nil {
			continue
		}

		if n == 1 {
			if first := s.search(name); first != nil {
				first.name += "1"
			}

			s.conflicted[name] = true
			n++
		}

		return name + strconv.Itoa(n)
	}
}

// fill writes the parameter and result types of im and chooses the names of its parameters.
// reserved holds the names that the generated method body uses itself.
//
// Types are written in the order moq registers imports in, so that the import names known when a
// parameter is named are the same as in moq.
func (n *namer) fill(im *iface.Method, reserved map[string]bool) {
	sc := &scope{imports: n.w.Imports, conflicted: map[string]bool{}}
	vars := make([]*variable, len(im.Params))

	for i := range im.Params {
		p := &im.Params[i]

		var refs []string

		p.Out, refs = n.w.TypeRefs(p.Expr, p.Ctx)
		vars[i] = sc.add(refs, n.varName(p.Name, p.Expr, p.Ctx, im.Variadic && i == len(im.Params)-1, ""))
	}

	for i := range im.Results {
		r := &im.Results[i]

		if r.Expr == nil {
			r.Out = r.Canon

			// The result of an embedded error's Error method is a string.
			name := r.Name
			if name == "" || name == "_" {
				name = basicName("string")
			}

			sc.add(nil, name+"Out")

			continue
		}

		var refs []string

		r.Out, refs = n.w.TypeRefs(r.Expr, r.Ctx)
		sc.add(refs, n.varName(r.Name, r.Expr, r.Ctx, false, "Out"))
	}

	taken := map[string]bool{}
	for _, v := range vars {
		taken[v.name] = true
	}

	used := map[string]bool{}
	seen := map[string]bool{}

	for i, v := range vars {
		p := &im.Params[i]

		// The field keeps moq's name. Only the variable may differ, because the body of the method
		// must still compile.
		p.Field = exported(v.name)
		for seen[p.Field] {
			p.Field += "_"
		}

		seen[p.Field] = true

		p.Var = v.name
		for reserved[p.Var] || used[p.Var] || (p.Var != v.name && taken[p.Var]) {
			p.Var += "_"
		}

		used[p.Var] = true
	}
}

// exported returns the field name for the variable s: the whole name is upper-cased if it is an
// initialism, otherwise only its first letter.
func exported(s string) string {
	if s == "" {
		return ""
	}

	if u := strings.ToUpper(s); slices.Contains(initialisms, u) {
		return u
	}

	return capitalise(s)
}

// varName is moq's varName: the name of a parameter, or a name derived from its type if it is
// unnamed or blank. e is the element type if variadic is set.
func (n *namer) varName(explicit string, e ast.Expr, c *canon.Ctx, variadic bool, suffix string) string {
	if explicit != "" && explicit != "_" {
		return explicit + suffix
	}

	var name string
	if variadic {
		name = n.nested(e, c) + "s"
	} else {
		name = n.typeName(e, c)
	}

	if name += suffix; moqReserved[name] {
		name += "MoqParam"
	}

	return name
}

// nested is the name of a type that is the element of a slice, array, map or channel: a basic
// type is named by itself.
func (n *namer) nested(e ast.Expr, c *canon.Ctx) string {
	if b, ok := n.basic(e, c); ok {
		return deCapitalise(b)
	}

	return n.typeName(e, c)
}

// typeName is moq's varNameForType. It cannot see through a type that go/types resolves and the
// syntax does not, so a name that is declared as an alias (type A = B) is named "v", as moq does.
// Where the declaration is in a package that cannot be read, the name is taken to be defined.
func (n *namer) typeName(e ast.Expr, c *canon.Ctx) string {
	if b, ok := n.basic(e, c); ok {
		return basicName(b)
	}

	switch t := e.(type) {
	case *ast.ParenExpr:
		return n.typeName(t.X, c)
	case *ast.Ident:
		return n.identName(t, c)
	case *ast.SelectorExpr:
		return n.selectorName(t, c)
	case *ast.IndexExpr:
		return n.typeName(t.X, c)
	case *ast.IndexListExpr:
		return n.typeName(t.X, c)
	case *ast.StarExpr:
		return n.typeName(t.X, c)
	case *ast.ArrayType:
		return n.nested(t.Elt, c) + "s"
	case *ast.MapType:
		return n.nested(t.Key, c) + "To" + capitalise(n.nested(t.Value, c))
	case *ast.ChanType:
		return n.nested(t.Value, c) + "Ch"
	case *ast.FuncType:
		return "fn"
	case *ast.StructType:
		return "val"
	case *ast.InterfaceType:
		return "ifaceVal"
	}

	return "v"
}

func (n *namer) identName(t *ast.Ident, c *canon.Ctx) string {
	if tp, ok := c.TP[t.Name]; ok {
		if tp.Arg != nil {
			return n.typeName(tp.Arg, tp.ArgCtx)
		}

		return "v" // a type parameter
	}

	pk := c.File.Pkg

	if td := pk.Types[t.Name]; td != nil {
		if td.IsAlias() {
			return "v"
		}

		return definedName(t.Name)
	}

	if pk.Names[t.Name] {
		return "v"
	}

	if dp, ok := n.l.DotLookup(c.File, t.Name); ok {
		if n.isAlias(dp, t.Name) {
			return "v"
		}

		return definedName(t.Name)
	}

	if t.Name == "error" {
		return "err"
	}

	return "v" // any, comparable
}

func (n *namer) selectorName(t *ast.SelectorExpr, c *canon.Ctx) string {
	x, ok := t.X.(*ast.Ident)
	if !ok {
		return "v"
	}

	ip, ok := n.l.ImportPath(c.File, x.Name)
	if !ok || n.isAlias(ip, t.Sel.Name) {
		return "v"
	}

	return definedName(t.Sel.Name)
}

// basic reports whether e denotes a predeclared basic type and returns its name. unsafe.Pointer
// is one too, written unsafePointer because moq's own spelling is not an identifier.
func (n *namer) basic(e ast.Expr, c *canon.Ctx) (string, bool) {
	switch t := e.(type) {
	case *ast.ParenExpr:
		return n.basic(t.X, c)
	case *ast.Ident:
		if tp, ok := c.TP[t.Name]; ok {
			if tp.Arg != nil {
				return n.basic(tp.Arg, tp.ArgCtx)
			}

			return "", false
		}

		if c.File.Pkg.Names[t.Name] || !isBasic(t.Name) {
			return "", false
		}

		return t.Name, true
	case *ast.SelectorExpr:
		x, ok := t.X.(*ast.Ident)
		if !ok || t.Sel.Name != "Pointer" {
			return "", false
		}

		if ip, ok := n.l.ImportPath(c.File, x.Name); ok && ip == "unsafe" {
			return "unsafePointer", true
		}
	}

	return "", false
}

// isAlias reports whether the type name declared in the package with import path p is an alias.
func (n *namer) isAlias(p, name string) bool {
	key := p + "." + name
	if v, ok := n.aliases[key]; ok {
		return v
	}

	var err error

	alias := false

	func() {
		defer load.Recover(&err)

		td := n.l.Package(p).Types[name]
		alias = td != nil && td.IsAlias()
	}()

	n.aliases[key] = alias

	return alias
}

// definedName is the name moq derives from a defined type: its name with a lower-case first
// letter, with MoqParam if that is not different from the type name.
func definedName(name string) string {
	d := deCapitalise(name)
	if d == name {
		d += "MoqParam"
	}

	return d
}

func isBasic(name string) bool {
	switch name {
	case "bool", "string", "byte", "rune", "uintptr",
		"int", "int8", "int16", "int32", "int64",
		"uint", "uint8", "uint16", "uint32", "uint64",
		"float32", "float64", "complex64", "complex128":
		return true
	}

	return false
}

// basicName is moq's basicTypeVarName. It tests the kind of the type for equality, so unsigned
// integers, uintptr, byte and complex numbers fall through to "v".
func basicName(b string) string {
	switch b {
	case "bool":
		return "b"
	case "int", "int8", "int16", "int32", "int64", "rune":
		return "n"
	case "float32", "float64":
		return "f"
	case "string":
		return "s"
	}

	return "v"
}

func capitalise(s string) string {
	r, size := utf8.DecodeRuneInString(s)

	return strings.ToUpper(string(r)) + s[size:]
}

func deCapitalise(s string) string {
	r, size := utf8.DecodeRuneInString(s)

	return strings.ToLower(string(r)) + s[size:]
}
