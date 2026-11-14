// Package load finds Go packages and reads their declarations from syntax only.
//
// Package directories are found from local files only (GOROOT/src, go.mod, go.work, vendor/ and
// the module cache); the go command is never started and nothing is downloaded. Files are
// selected with go/build and parsed with go/parser. Nothing is type-checked, which keeps loading
// fast and independent of whether the package compiles.
//
// Failures are reported with Failf, which panics with *Error; entry points of the other internal
// packages convert it back with Recover.
package load

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
)

// Error is a user-facing failure raised by Failf.
type Error struct {
	Msg string
}

func (e *Error) Error() string { return e.Msg }

// Failf aborts the current operation with a *Error. The nearest deferred Recover turns it into
// an error value.
func Failf(format string, args ...any) {
	panic(&Error{Msg: fmt.Sprintf(format, args...)})
}

// Recover stores a panic raised by Failf in *err. It must be deferred directly:
//
//	defer load.Recover(&err)
//
// Any other panic is propagated.
func Recover(err *error) {
	x := recover()
	if x == nil {
		return
	}

	e, ok := x.(*Error)
	if !ok {
		panic(x)
	}

	*err = e
}

// ImportSpec is one import declaration of a file.
type ImportSpec struct {
	Alias string // "" if the import is not renamed
	Path  string
}

// File is a parsed source file and the names its imports bring into scope.
type File struct {
	Pkg     *Package
	AST     *ast.File
	Imports []ImportSpec
	Dots    []string // dot-imported paths
}

// TypeDecl is a package-level type declaration.
type TypeDecl struct {
	Spec *ast.TypeSpec
	File *File
}

// IsAlias reports whether the declaration is an alias (type A = B).
func (d *TypeDecl) IsAlias() bool { return d.Spec.Assign.IsValid() }

// ConstDecl is a package-level constant. Expr is the expression that defines it, which for a
// constant that repeats the previous specification is the expression of that specification.
type ConstDecl struct {
	Expr ast.Expr
	Iota int // value of iota in Expr
	File *File
}

// Package is the syntax of one package.
type Package struct {
	Path   string
	Name   string
	Files  []*File
	Types  map[string]*TypeDecl
	Consts map[string]*ConstDecl
	Names  map[string]bool // every package-level identifier
}

// Loader resolves and caches packages. It is not safe for concurrent use.
type Loader struct {
	Fset *token.FileSet

	dir   string
	res   *resolver
	dirs  map[string]string
	pkgs  map[string]*Package
	build map[string]*build.Package // import path -> files selected by go/build
}

// Option configures a Loader.
type Option func(*options)

type options struct {
	getenv func(string) string
}

// WithGetenv sets the function that reads environment variables (GOROOT, GOMODCACHE, GOPATH,
// GOFLAGS, GOWORK, GOENV). The default is os.Getenv. PATH is always read from the process.
func WithGetenv(getenv func(string) string) Option {
	return func(o *options) { o.getenv = getenv }
}

// New returns a Loader that resolves packages relative to dir ("" for the current directory).
func New(dir string, opts ...Option) (*Loader, error) {
	if dir != "" {
		abs, err := filepath.Abs(dir)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", dir, err)
		}

		dir = abs
	}

	o := options{getenv: os.Getenv}
	for _, opt := range opts {
		opt(&o)
	}

	return &Loader{
		Fset:  token.NewFileSet(),
		dir:   dir,
		res:   newResolver(dir, o.getenv),
		dirs:  map[string]string{},
		pkgs:  map[string]*Package{},
		build: map[string]*build.Package{},
	}, nil
}

// Pos formats a source position for messages.
func (l *Loader) Pos(p token.Pos) string {
	pos := l.Fset.Position(p)
	if l.dir != "" {
		if rel, err := filepath.Rel(l.dir, pos.Filename); err == nil && !strings.HasPrefix(rel, "..") {
			pos.Filename = rel
		}
	}

	return pos.String()
}

func isDir(p string) bool {
	st, err := os.Stat(p)

	return err == nil && st.IsDir()
}

// ResolveDir returns the import path of the package in directory dir (relative to the loader's
// working directory).
func (l *Loader) ResolveDir(dir string) (string, error) {
	abs := dir
	if !filepath.IsAbs(abs) {
		abs = filepath.Join(l.dir, abs)
	}

	if !isDir(abs) {
		return "", fmt.Errorf("cannot find package in %q (no such directory)", dir)
	}

	abs, err := filepath.Abs(abs)
	if err != nil {
		return "", fmt.Errorf("cannot find package in %q: %w", dir, err)
	}

	ip, err := l.res.importPathOf(abs)
	if err != nil {
		return "", fmt.Errorf("cannot find package in %q (%w)", dir, err)
	}

	return ip, nil
}

func (l *Loader) dirOf(p string) string {
	if d, ok := l.dirs[p]; ok {
		return d
	}

	d, err := l.res.dirOf(p)
	if err != nil {
		Failf("%v", err)
	}

	l.dirs[p] = d

	return d
}

// buildPackage returns the go/build view of the package with import path p; the result is cached.
func (l *Loader) buildPackage(p string) *build.Package {
	if bp, ok := l.build[p]; ok {
		return bp
	}

	bp, err := build.ImportDir(l.dirOf(p), 0)
	if err != nil && len(bp.GoFiles) == 0 {
		Failf("package %q: %v", p, err)
	}

	l.build[p] = bp

	return bp
}

// PackageName returns the package clause name of an import path.
func (l *Loader) PackageName(p string) string {
	return l.buildPackage(p).Name
}

// Package parses the package with import path p; the result is cached.
func (l *Loader) Package(p string) *Package {
	if pi, ok := l.pkgs[p]; ok {
		return pi
	}

	bp := l.buildPackage(p)
	dir := l.dirOf(p)

	pi := &Package{
		Path:   p,
		Name:   bp.Name,
		Types:  map[string]*TypeDecl{},
		Consts: map[string]*ConstDecl{},
		Names:  map[string]bool{},
	}
	l.pkgs[p] = pi

	for _, fn := range bp.GoFiles {
		f, err := parser.ParseFile(l.Fset, filepath.Join(dir, fn), nil, parser.SkipObjectResolution)
		if err != nil {
			Failf("%v", err)
		}

		fi := &File{Pkg: pi, AST: f}
		fi.addImports()
		fi.declare()
		pi.Files = append(pi.Files, fi)
	}

	return pi
}

// addImports records the imports of the file.
func (fi *File) addImports() {
	for _, is := range fi.AST.Imports {
		ip, _ := strconv.Unquote(is.Path.Value)

		switch {
		case is.Name == nil:
			fi.Imports = append(fi.Imports, ImportSpec{Path: ip})
		case is.Name.Name == ".":
			fi.Dots = append(fi.Dots, ip)
		case is.Name.Name == "_":
		default:
			fi.Imports = append(fi.Imports, ImportSpec{Alias: is.Name.Name, Path: ip})
		}
	}
}

// declare registers the package-level declarations of the file.
func (fi *File) declare() {
	pi := fi.Pkg

	for _, d := range fi.AST.Decls {
		switch d := d.(type) {
		case *ast.GenDecl:
			var implicit *ast.ValueSpec // the specification a constant without values repeats

			for i, s := range d.Specs {
				switch s := s.(type) {
				case *ast.TypeSpec:
					pi.Types[s.Name.Name] = &TypeDecl{Spec: s, File: fi}
					pi.Names[s.Name.Name] = true
				case *ast.ValueSpec:
					if d.Tok == token.CONST && len(s.Values) > 0 {
						implicit = s
					}

					for j, n := range s.Names {
						pi.Names[n.Name] = true

						if d.Tok == token.CONST && implicit != nil && j < len(implicit.Values) {
							pi.Consts[n.Name] = &ConstDecl{Expr: implicit.Values[j], Iota: i, File: fi}
						}
					}
				}
			}
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name != "init" && d.Name.Name != "_" {
				pi.Names[d.Name.Name] = true
			}
		}
	}
}

// ImportPath resolves a package qualifier used in file fi to an import path.
func (l *Loader) ImportPath(fi *File, name string) (string, bool) {
	for _, is := range fi.Imports {
		if is.Alias == name {
			return is.Path, true
		}
	}

	// Unaliased: the qualifier is the package clause name of the imported package. Try the
	// obvious candidates first (last path element), then everything else; always verify.
	var rest []ImportSpec

	for _, is := range fi.Imports {
		if is.Alias != "" {
			continue
		}

		if path.Base(is.Path) == name {
			if l.PackageName(is.Path) == name {
				return is.Path, true
			}

			continue
		}

		rest = append(rest, is)
	}

	for _, is := range rest {
		if l.PackageName(is.Path) == name {
			return is.Path, true
		}
	}

	return "", false
}

// DotLookup finds which dot-imported package of fi provides the exported identifier name.
func (l *Loader) DotLookup(fi *File, name string) (string, bool) {
	if !ast.IsExported(name) {
		return "", false
	}

	for _, dp := range fi.Dots {
		if l.Package(dp).Names[name] {
			return dp, true
		}
	}

	return "", false
}

// ModulePath derives the import path of the directory holding file from the nearest go.mod. It
// returns "" if there is none.
func ModulePath(file string) string {
	abs, err := filepath.Abs(file)
	if err != nil {
		return ""
	}

	dir := filepath.Dir(abs)

	root := findUp(dir, "go.mod")
	if root == "" {
		return ""
	}

	b, err := os.ReadFile(filepath.Join(root, "go.mod"))
	if err != nil {
		return ""
	}

	p := modfile.ModulePath(b)
	if p == "" {
		return ""
	}

	if rel, err := filepath.Rel(root, dir); err == nil && rel != "." {
		p += "/" + filepath.ToSlash(rel)
	}

	return p
}
