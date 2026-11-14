package load

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"

	"golang.org/x/mod/modfile"
	"golang.org/x/mod/module"
	"golang.org/x/mod/semver"
)

// goEnv looks up go environment settings the way the go command does: a non-empty process
// environment variable wins over the go env file written by `go env -w`.
type goEnv struct {
	getenv func(string) string
	file   map[string]string
}

func newGoEnv(getenv func(string) string) *goEnv {
	return &goEnv{getenv: getenv, file: readEnvFile(getenv("GOENV"))}
}

func (e *goEnv) get(key string) string {
	if v := e.getenv(key); v != "" {
		return v
	}

	return e.file[key]
}

// readEnvFile parses the go env file. goenv is the value of $GOENV: "off" disables the file and
// "" selects the default location.
func readEnvFile(goenv string) map[string]string {
	if goenv == "off" {
		return nil
	}

	if goenv == "" {
		cfg, err := os.UserConfigDir()
		if err != nil {
			return nil
		}

		goenv = filepath.Join(cfg, "go", "env")
	}

	b, err := os.ReadFile(filepath.Clean(goenv))
	if err != nil {
		return nil
	}

	m := map[string]string{}

	for ln := range strings.SplitSeq(string(b), "\n") {
		ln = strings.TrimSuffix(ln, "\r")
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}

		if k, v, ok := strings.Cut(ln, "="); ok && k != "" {
			m[k] = v
		}
	}

	return m
}

// goroot locates the Go installation without starting the go command: $GOROOT, else two
// directories above the go binary found on PATH (the file is looked up, never executed), else
// the root this binary was built with.
func (e *goEnv) goroot() string {
	if v := e.get("GOROOT"); v != "" {
		return filepath.Clean(v)
	}

	if p, err := exec.LookPath("go"); err == nil {
		if target, err := filepath.EvalSymlinks(p); err == nil {
			if root := filepath.Dir(filepath.Dir(target)); isDir(filepath.Join(root, "src")) {
				return root
			}
		}
	}

	return runtime.GOROOT()
}

// modcache returns the module cache directory.
func (e *goEnv) modcache() string {
	if v := e.get("GOMODCACHE"); v != "" {
		return filepath.Clean(v)
	}

	if gp := filepath.SplitList(e.get("GOPATH")); len(gp) > 0 && gp[0] != "" {
		return filepath.Join(gp[0], "pkg", "mod")
	}

	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}

	return filepath.Join(home, "go", "pkg", "mod")
}

// flag returns the value of the last -name=value (or --name=value) in GOFLAGS.
func (e *goEnv) flag(name string) string {
	var val string

	for f := range strings.FieldsSeq(e.get("GOFLAGS")) {
		f = strings.TrimPrefix(strings.TrimPrefix(f, "-"), "-")
		if k, v, ok := strings.Cut(f, "="); ok && k == name {
			val = v
		}
	}

	return val
}

// mainModule is a module whose files are the user's own: the main module or a workspace member.
type mainModule struct {
	path string
	dir  string
}

// replacement is the target of a replace directive: a directory, or another module version.
type replacement struct {
	dir  string // local directory, absolute; "" for a module replacement
	path string
	ver  string
}

// resolver maps import paths to directories using only local files: GOROOT/src, go.mod, go.work,
// vendor/ and the module cache. It never runs the go command and never downloads anything.
type resolver struct {
	env      *goEnv
	dir      string // working directory; "" for the process's
	goroot   string
	modcache string

	loaded   bool
	loadErr  error
	mains    []mainModule
	requires map[string]string              // module path -> highest required version
	replaces map[module.Version]replacement // key Version is "" for a versionless replace
	vendor   string                         // vendor directory when in vendor mode
}

func newResolver(dir string, getenv func(string) string) *resolver {
	env := newGoEnv(getenv)

	return &resolver{env: env, dir: dir, goroot: env.goroot(), modcache: env.modcache()}
}

// stdDir returns the GOROOT directory of a standard library package.
func (r *resolver) stdDir(p string) (string, bool) {
	if !IsStd(p) {
		return "", false
	}

	d := filepath.Join(r.goroot, "src", filepath.FromSlash(p))

	return d, isDir(d)
}

// dirOf returns the directory of the package with import path p.
func (r *resolver) dirOf(p string) (string, error) {
	if d, ok := r.stdDir(p); ok {
		return d, nil
	}

	if err := r.load(); err != nil {
		return "", err
	}

	return r.lookup(p)
}

// candidate is a module that might provide a package.
type candidate struct {
	path string
	main bool
	dir  string // directory of a main module
	ver  string
}

// lookup finds p in the main modules, the vendor directory or the required modules.
func (r *resolver) lookup(p string) (string, error) {
	var cands []candidate

	for _, m := range r.mains {
		if hasPathPrefix(p, m.path) {
			cands = append(cands, candidate{path: m.path, main: true, dir: m.dir})
		}
	}

	if r.vendor == "" {
		for path, ver := range r.requires {
			if hasPathPrefix(p, path) {
				cands = append(cands, candidate{path: path, ver: ver})
			}
		}
	}

	// Longest module path first; a main module wins a tie.
	slices.SortFunc(cands, func(a, b candidate) int {
		if c := len(b.path) - len(a.path); c != 0 {
			return c
		}

		if a.main != b.main {
			if a.main {
				return -1
			}

			return 1
		}

		return strings.Compare(a.path, b.path)
	})

	var missing, found string

	for _, c := range cands {
		root, miss, err := r.moduleRoot(c)
		if err != nil {
			return "", err
		}

		if miss != "" {
			if missing == "" {
				missing = miss
			}

			continue
		}

		if found == "" {
			found = c.path
		}

		if d := filepath.Join(root, filepath.FromSlash(strings.TrimPrefix(strings.TrimPrefix(p, c.path), "/"))); isDir(d) {
			return d, nil
		}
	}

	if r.vendor != "" {
		if d := filepath.Join(r.vendor, filepath.FromSlash(p)); isDir(d) {
			return d, nil
		}

		return "", fmt.Errorf("package %q is not in the vendor directory; run \"go mod vendor\"", p)
	}

	switch {
	case missing != "":
		mod, ver, _ := strings.Cut(missing, "@")

		return "", fmt.Errorf("module %s@%s is not in the module cache; run \"go mod download\"", mod, ver)
	case found != "":
		return "", fmt.Errorf("cannot find package %q in module %s", p, found)
	}

	return "", fmt.Errorf("no required module provides package %q", p)
}

// moduleRoot returns the directory of candidate c. miss is "path@version" when the module is
// not in the module cache.
func (r *resolver) moduleRoot(c candidate) (root, miss string, err error) {
	if c.main {
		return c.dir, "", nil
	}

	path, ver := c.path, c.ver

	rep, ok := r.replaces[module.Version{Path: c.path, Version: c.ver}]
	if !ok {
		rep, ok = r.replaces[module.Version{Path: c.path}]
	}

	if ok {
		if rep.dir != "" {
			if !isDir(rep.dir) {
				return "", "", fmt.Errorf("replacement directory %s of module %s does not exist", rep.dir, c.path)
			}

			return rep.dir, "", nil
		}

		path, ver = rep.path, rep.ver
		if ver == "" {
			ver = c.ver
		}
	}

	root = r.cacheDir(path, ver)
	if root == "" || !isDir(root) {
		return "", path + "@" + ver, nil
	}

	return root, "", nil
}

// cacheDir returns where module path@ver is extracted in the module cache, or "" if it has no
// valid location.
func (r *resolver) cacheDir(path, ver string) string {
	ep, err := module.EscapePath(path)
	if err != nil {
		return ""
	}

	ev, err := module.EscapeVersion(ver)
	if err != nil {
		return ""
	}

	return filepath.Join(r.modcache, filepath.FromSlash(ep)+"@"+ev)
}

// IsStd reports whether p looks like a standard library import path: its first element has no
// dot.
func IsStd(p string) bool {
	first, _, _ := strings.Cut(p, "/")

	return !strings.Contains(first, ".")
}

func hasPathPrefix(p, prefix string) bool {
	return p == prefix || strings.HasPrefix(p, prefix+"/")
}

// load reads go.work and go.mod once. The result of the first call is returned every time.
func (r *resolver) load() error {
	if r.loaded {
		return r.loadErr
	}

	r.loaded = true
	r.requires = map[string]string{}
	r.replaces = map[module.Version]replacement{}

	r.loadErr = r.loadModules()

	return r.loadErr
}

func (r *resolver) loadModules() error {
	dir := r.dir
	if dir == "" {
		wd, err := os.Getwd()
		if err != nil {
			return fmt.Errorf("get working directory: %w", err)
		}

		dir = wd
	}

	if work := r.findGoWork(dir); work != "" {
		return r.loadWorkspace(work)
	}

	root := findUp(dir, "go.mod")
	if root == "" {
		return nil // outside a module: only the standard library resolves
	}

	mf, err := readGoMod(root)
	if err != nil {
		return err
	}

	r.addModule(root, mf, nil)
	r.setVendor(root, mf)

	return nil
}

// findGoWork returns the go.work file in effect, or "" when workspace mode is off.
func (r *resolver) findGoWork(dir string) string {
	switch v := r.env.get("GOWORK"); v {
	case "off":
		return ""
	case "":
		if root := findUp(dir, "go.work"); root != "" {
			return filepath.Join(root, "go.work")
		}

		return ""
	default:
		if !filepath.IsAbs(v) {
			v = filepath.Join(dir, v)
		}

		return v
	}
}

func (r *resolver) loadWorkspace(file string) error {
	b, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		return fmt.Errorf("read go.work: %w", err)
	}

	wf, err := modfile.ParseWork(file, b, nil)
	if err != nil {
		return fmt.Errorf("parse go.work: %w", err)
	}

	workDir := filepath.Dir(file)

	// go.work replaces override go.mod replaces of the same module path.
	override := map[string]bool{}
	for _, rep := range wf.Replace {
		override[rep.Old.Path] = true
	}

	for _, u := range wf.Use {
		root := filepath.Join(workDir, filepath.FromSlash(u.Path))
		if filepath.IsAbs(filepath.FromSlash(u.Path)) {
			root = filepath.FromSlash(u.Path)
		}

		mf, err := readGoMod(root)
		if err != nil {
			return err
		}

		r.addModule(root, mf, override)
	}

	for _, rep := range wf.Replace {
		r.addReplace(workDir, rep)
	}

	return nil
}

// readGoMod parses root/go.mod.
func readGoMod(root string) (*modfile.File, error) {
	file := filepath.Join(root, "go.mod")

	b, err := os.ReadFile(filepath.Clean(file))
	if err != nil {
		return nil, fmt.Errorf("read go.mod: %w", err)
	}

	mf, err := modfile.Parse(file, b, nil)
	if err != nil {
		return nil, fmt.Errorf("parse go.mod: %w", err)
	}

	if mf.Module == nil {
		return nil, fmt.Errorf("parse go.mod: %s has no module directive", file)
	}

	return mf, nil
}

// addModule registers the main module in root and its requirements. Replaces of the module
// paths in skip are ignored.
func (r *resolver) addModule(root string, mf *modfile.File, skip map[string]bool) {
	r.mains = append(r.mains, mainModule{path: mf.Module.Mod.Path, dir: root})

	for _, req := range mf.Require {
		if cur, ok := r.requires[req.Mod.Path]; !ok || semver.Compare(req.Mod.Version, cur) > 0 {
			r.requires[req.Mod.Path] = req.Mod.Version
		}
	}

	for _, rep := range mf.Replace {
		if skip[rep.Old.Path] {
			continue
		}

		if _, dup := r.replaces[rep.Old]; !dup {
			r.addReplace(root, rep)
		}
	}
}

// addReplace records a replace directive found in a file located in dir.
func (r *resolver) addReplace(dir string, rep *modfile.Replace) {
	if modfile.IsDirectoryPath(rep.New.Path) {
		p := filepath.FromSlash(rep.New.Path)
		if !filepath.IsAbs(p) {
			p = filepath.Join(dir, p)
		}

		r.replaces[rep.Old] = replacement{dir: p}

		return
	}

	r.replaces[rep.Old] = replacement{path: rep.New.Path, ver: rep.New.Version}
}

// setVendor enables vendor mode when the go command would: -mod=vendor, or by default a go
// directive of 1.14 or later together with vendor/modules.txt. Workspaces never use it.
func (r *resolver) setVendor(root string, mf *modfile.File) {
	dir := filepath.Join(root, "vendor")

	switch r.env.flag("mod") {
	case "vendor":
		r.vendor = dir
	case "":
		ver := "1.16" // the go command's assumption for a go.mod without a go directive
		if mf.Go != nil {
			ver = mf.Go.Version
		}

		if goAtLeast(ver, 1, 14) && isFile(filepath.Join(dir, "modules.txt")) {
			r.vendor = dir
		}
	}
}

// goAtLeast reports whether the go directive version v is at least major.minor.
func goAtLeast(v string, major, minor int) bool {
	maj, rest, _ := strings.Cut(v, ".")

	mj, err := strconv.Atoi(maj)
	if err != nil {
		return false
	}

	if mj != major {
		return mj > major
	}

	digits := rest
	for i, c := range rest {
		if c < '0' || c > '9' {
			digits = rest[:i]

			break
		}
	}

	mn, err := strconv.Atoi(digits)

	return err == nil && mn >= minor
}

// importPathOf derives the import path of the existing directory abs from the main modules.
func (r *resolver) importPathOf(abs string) (string, error) {
	if err := r.load(); err != nil {
		return "", err
	}

	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		resolved = abs
	}

	root := findUp(resolved, "go.mod")
	if root == "" {
		return "", errors.New("outside a module")
	}

	for _, m := range r.mains {
		md, err := filepath.EvalSymlinks(m.dir)
		if err != nil {
			md = m.dir
		}

		if md != root {
			continue
		}

		rel, err := filepath.Rel(root, resolved)
		if err != nil {
			break
		}

		if rel == "." {
			return m.path, nil
		}

		return m.path + "/" + filepath.ToSlash(rel), nil
	}

	return "", errors.New("outside the main module")
}

// findUp returns the nearest directory at or above dir that contains a file named name.
func findUp(dir, name string) string {
	for d := dir; ; d = filepath.Dir(d) {
		if isFile(filepath.Join(d, name)) {
			return d
		}

		if filepath.Dir(d) == d {
			return ""
		}
	}
}

func isFile(p string) bool {
	st, err := os.Stat(p)

	return err == nil && !st.IsDir()
}
