package load_test

import (
	"maps"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/sivchari/mimic/internal/load"
)

const (
	modresDir    = "../../testdata/modres"
	modresVendor = "../../testdata/modres-vendor"
	modresWork   = "../../testdata/modres-work"
)

// env returns a Getenv that serves m and hides the user's go env file.
func env(m map[string]string) func(string) string {
	return func(k string) string {
		if k == "GOENV" {
			return "off"
		}

		return m[k]
	}
}

func abs(t *testing.T, p string) string {
	t.Helper()

	a, err := filepath.Abs(p)
	if err != nil {
		t.Fatal(err)
	}

	return a
}

func newModres(t *testing.T, extra map[string]string) *load.Loader {
	t.Helper()

	m := map[string]string{
		"GOROOT":     runtime.GOROOT(),
		"GOMODCACHE": abs(t, filepath.Join(modresDir, "modcache")),
	}
	maps.Copy(m, extra)

	return newLoaderAt(t, modresDir, m)
}

func newLoaderAt(t *testing.T, dir string, m map[string]string) *load.Loader {
	t.Helper()

	l, err := load.New(dir, load.WithGetenv(env(m)))
	if err != nil {
		t.Fatal(err)
	}

	return l
}

// loadErr returns the message of the failure raised while loading path, or "".
func loadErr(l *load.Loader, path string) (msg string) {
	var err error

	func() {
		defer load.Recover(&err)

		l.Package(path)
	}()

	if err != nil {
		return err.Error()
	}

	return ""
}

func TestResolve_Packages(t *testing.T) {
	t.Parallel()

	// Each package declares a type that only that directory has, so a hit proves which
	// directory was read.
	tests := []struct {
		name string
		path string
		typ  string
	}{
		{"standard library", "io", "Reader"},
		{"main module root", "example.com/modres", "Store"},
		{"main module subpackage", "example.com/modres/sub", "InMain"},
		{"module cache, highest required version", "example.com/cached", "Item"},
		{"module cache subpackage", "example.com/cached/inner", "V12"},
		{"escaped module path with capital letters", "example.com/Upper", "Doc"},
		{"replace to a local directory", "example.com/localrep", "Piece"},
		{"subpackage of a local replace", "example.com/localrep/deep", "FromLocalDeep"},
		{"replace to another module", "example.com/orig", "Fork"},
		{"versionless replace to another module", "example.com/anyv", "Fork"},
		{"subpackage of a module replace", "example.com/orig/pkg", "ForkPkg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// A Loader is not safe for concurrent use.
			l := newModres(t, nil)

			if p := l.Package(tt.path); p.Types[tt.typ] == nil {
				t.Fatalf("%s: no type %s in %v", tt.path, tt.typ, p.Names)
			}
		})
	}
}

func TestResolve_Errors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		path string
		want string
	}{
		{"not required", "example.com/unrequired/x", `no required module provides package "example.com/unrequired/x"`},
		{"not in the module cache", "example.com/missing/x", `module example.com/missing@v1.0.0 is not in the module cache; run "go mod download"`},
		{"required but no such directory", "example.com/cached/nope", `cannot find package "example.com/cached/nope"`},
		{"main module without the directory", "example.com/modres/nope", `cannot find package "example.com/modres/nope"`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if got := loadErr(newModres(t, nil), tt.path); !strings.Contains(got, tt.want) {
				t.Fatalf("got %q, want it to contain %q", got, tt.want)
			}
		})
	}
}

func TestResolve_EmptyModuleCache(t *testing.T) {
	t.Parallel()

	l := newModres(t, map[string]string{"GOMODCACHE": t.TempDir()})

	want := `module example.com/cached@v1.2.0 is not in the module cache; run "go mod download"`
	if got := loadErr(l, "example.com/cached"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// The replaced module names the replacement, which is what has to be downloaded.
	want = `module example.com/forked@v1.5.0 is not in the module cache; run "go mod download"`
	if got := loadErr(l, "example.com/orig"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	// A local replace needs no cache.
	if l.Package("example.com/localrep").Types["Piece"] == nil {
		t.Fatal("local replace must not use the module cache")
	}
}

func TestResolve_ModuleCacheLocation(t *testing.T) {
	cache := abs(t, filepath.Join(modresDir, "modcache"))

	tests := []struct {
		name string
		env  map[string]string
	}{
		{"GOMODCACHE", map[string]string{"GOMODCACHE": cache}},
		{"first GOPATH entry", map[string]string{"GOPATH": cache + "/../gopath" + string(os.PathListSeparator) + "/unused"}},
	}

	// GOPATH/pkg/mod is not the fixture cache, so only GOMODCACHE must find the package; the
	// GOPATH case checks that the derived location is the one reported.
	l := newLoaderAt(t, modresDir, tests[0].env)
	if l.Package("example.com/cached").Types["Item"] == nil {
		t.Fatal("GOMODCACHE not honored")
	}

	l = newLoaderAt(t, modresDir, tests[1].env)

	got := loadErr(l, "example.com/cached")
	if !strings.Contains(got, `module example.com/cached@v1.2.0 is not in the module cache`) {
		t.Fatalf("got %q", got)
	}

	// GOPATH/pkg/mod layout works too.
	gp := t.TempDir()
	copyDir(t, filepath.Join(cache, "example.com"), filepath.Join(gp, "pkg", "mod", "example.com"))

	l = newLoaderAt(t, modresDir, map[string]string{"GOPATH": gp, "GOROOT": runtime.GOROOT()})
	if l.Package("example.com/cached").Types["Item"] == nil {
		t.Fatal("GOPATH/pkg/mod not honored")
	}

	// Default: $HOME/go/pkg/mod.
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	copyDir(t, filepath.Join(cache, "example.com"), filepath.Join(home, "go", "pkg", "mod", "example.com"))

	l = newLoaderAt(t, modresDir, map[string]string{"GOROOT": runtime.GOROOT()})
	if l.Package("example.com/cached").Types["Item"] == nil {
		t.Fatal("default ~/go/pkg/mod not honored")
	}
}

func copyDir(t *testing.T, src, dst string) {
	t.Helper()

	err := filepath.WalkDir(src, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		rel, _ := filepath.Rel(src, p)
		if d.IsDir() {
			return os.MkdirAll(filepath.Join(dst, rel), 0o750)
		}

		b, err := os.ReadFile(filepath.Clean(p))
		if err != nil {
			return err
		}

		return os.WriteFile(filepath.Join(dst, rel), b, 0o600)
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestResolve_Vendor(t *testing.T) {
	t.Parallel()

	emptyCache := t.TempDir()

	tests := []struct {
		name    string
		flags   string
		wantErr string
	}{
		{name: "default for go 1.14+ with modules.txt"},
		{name: "-mod=vendor", flags: "-mod=vendor"},
		{name: "other flags are kept", flags: "-trimpath -mod=vendor"},
		{name: "-mod=mod ignores vendor", flags: "-mod=mod", wantErr: `module example.com/dep@v1.0.0 is not in the module cache; run "go mod download"`},
		{name: "-mod=readonly ignores vendor", flags: "-mod=readonly", wantErr: `module example.com/dep@v1.0.0 is not in the module cache`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			l := newLoaderAt(t, modresVendor, map[string]string{
				"GOROOT": runtime.GOROOT(), "GOMODCACHE": emptyCache, "GOFLAGS": tt.flags,
			})

			got := loadErr(l, "example.com/dep")
			if tt.wantErr == "" {
				if got != "" || l.Package("example.com/dep").Types["Thing"] == nil {
					t.Fatalf("want vendored package, got %q", got)
				}

				return
			}

			if !strings.Contains(got, tt.wantErr) {
				t.Fatalf("got %q, want %q", got, tt.wantErr)
			}
		})
	}
}

func TestResolve_VendorMissingPackage(t *testing.T) {
	t.Parallel()

	l := newLoaderAt(t, modresVendor, map[string]string{"GOROOT": runtime.GOROOT(), "GOMODCACHE": t.TempDir()})

	got := loadErr(l, "example.com/dep/other")
	if !strings.Contains(got, `is not in the vendor directory; run "go mod vendor"`) {
		t.Fatalf("got %q", got)
	}

	// Main module packages are never vendored.
	if got := loadErr(l, "example.com/modresv"); got != "" {
		t.Fatalf("main module: %q", got)
	}
}

func TestResolve_VendorModeDecision(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		goLine  string
		modules bool
		flags   string
		vendor  bool
	}{
		{"go 1.14 with modules.txt", "go 1.14", true, "", true},
		{"go 1.13 predates automatic vendoring", "go 1.13", false, "", false},
		{"go 1.13 with modules.txt", "go 1.13", true, "", false},
		{"go 1.13 with -mod=vendor", "go 1.13", true, "-mod=vendor", true},
		{"no go directive counts as 1.16", "", true, "", true},
		{"no modules.txt", "go 1.26", false, "", false},
		{"patch version", "go 1.26.1", true, "", true},
		{"no modules.txt, -mod=vendor", "go 1.26", false, "-mod=vendor", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			root := t.TempDir()
			writeFile(t, root, "go.mod", "module example.com/m\n\n"+tt.goLine+"\n\nrequire example.com/dep v1.0.0\n")
			writeFile(t, root, "vendor/example.com/dep/dep.go", "package dep\n")

			cache := t.TempDir()
			writeFile(t, cache, "example.com/dep@v1.0.0/dep.go", "package dep\n\ntype FromCache struct{}\n")

			if tt.modules {
				writeFile(t, root, "vendor/modules.txt", "# example.com/dep v1.0.0\nexample.com/dep\n")
			}

			l := newLoaderAt(t, root, map[string]string{
				"GOROOT": runtime.GOROOT(), "GOMODCACHE": cache, "GOFLAGS": tt.flags,
			})

			// The vendored copy has no types; the cached one has FromCache.
			p := l.Package("example.com/dep")
			if fromCache := p.Types["FromCache"] != nil; fromCache == tt.vendor {
				t.Fatalf("vendor mode = %v, but cache used = %v", tt.vendor, fromCache)
			}
		})
	}
}

func writeFile(t *testing.T, dir, name, content string) {
	t.Helper()

	p := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(p), 0o750); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestResolve_Workspace(t *testing.T) {
	t.Parallel()

	m := map[string]string{"GOROOT": runtime.GOROOT(), "GOMODCACHE": abs(t, filepath.Join(modresDir, "modcache"))}

	for _, start := range []string{"a", "b"} {
		l := newLoaderAt(t, filepath.Join(modresWork, start), m)

		tests := []struct {
			path string
			typ  string
		}{
			{"example.com/work/a", "InA"},
			{"example.com/work/b", "InB"},
			// b requires v1.2.0 and a v1.1.0: the highest wins.
			{"example.com/cached/inner", "V12"},
			// go.work replace beats the replace in a/go.mod.
			{"example.com/over", "FromWorkReplace"},
		}

		for _, tt := range tests {
			if p := l.Package(tt.path); p.Types[tt.typ] == nil {
				t.Errorf("from %s: %s has no type %s", start, tt.path, tt.typ)
			}
		}
	}
}

func TestResolve_WorkspaceOff(t *testing.T) {
	t.Parallel()

	m := map[string]string{
		"GOROOT": runtime.GOROOT(), "GOMODCACHE": abs(t, filepath.Join(modresDir, "modcache")), "GOWORK": "off",
	}
	l := newLoaderAt(t, filepath.Join(modresWork, "b"), m)

	// Without the workspace, module a is an ordinary requirement that is not in the cache.
	want := `module example.com/work/a@v0.0.0 is not in the module cache; run "go mod download"`
	if got := loadErr(l, "example.com/work/a"); got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	if l.Package("example.com/work/b").Types["InB"] == nil {
		t.Fatal("main module package")
	}

	if _, err := l.ResolveDir("../a"); err == nil {
		t.Fatal("a is not part of the build without the workspace")
	}
}

func TestResolve_GOWORKPath(t *testing.T) {
	t.Parallel()

	work := abs(t, filepath.Join(modresWork, "go.work"))
	m := map[string]string{"GOROOT": runtime.GOROOT(), "GOMODCACHE": t.TempDir(), "GOWORK": work}

	// The working directory is outside the workspace; GOWORK still selects it.
	l := newLoaderAt(t, modresVendor, m)
	if l.Package("example.com/work/a").Types["InA"] == nil {
		t.Fatal("GOWORK file not used")
	}
}

func TestResolveDir_Modules(t *testing.T) {
	t.Parallel()

	l := newLoaderAt(t, modresDir, map[string]string{"GOROOT": runtime.GOROOT(), "GOENV": "off"})

	tests := []struct {
		dir  string
		want string
	}{
		{".", "example.com/modres"},
		{"./sub", "example.com/modres/sub"},
		{abs(t, filepath.Join(modresDir, "sub")), "example.com/modres/sub"},
	}

	for _, tt := range tests {
		if got, err := l.ResolveDir(tt.dir); err != nil || got != tt.want {
			t.Errorf("ResolveDir(%q) = %q, %v; want %q", tt.dir, got, err, tt.want)
		}
	}

	for _, dir := range []string{"./nope", "./replaced/localrep", "../modres-vendor"} {
		if got, err := l.ResolveDir(dir); err == nil {
			t.Errorf("ResolveDir(%q) = %q, want an error (missing or outside the main module)", dir, got)
		}
	}
}

func TestResolveDir_Workspace(t *testing.T) {
	t.Parallel()

	l := newLoaderAt(t, filepath.Join(modresWork, "b"), map[string]string{"GOROOT": runtime.GOROOT()})

	for dir, want := range map[string]string{".": "example.com/work/b", "../a": "example.com/work/a"} {
		if got, err := l.ResolveDir(dir); err != nil || got != want {
			t.Errorf("ResolveDir(%q) = %q, %v; want %q", dir, got, err, want)
		}
	}
}

func TestResolve_OutsideModule(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	l := newLoaderAt(t, dir, map[string]string{"GOROOT": runtime.GOROOT()})

	if l.Package("io").Types["Reader"] == nil {
		t.Fatal("the standard library resolves without a module")
	}

	if got := loadErr(l, "example.com/x"); !strings.Contains(got, `no required module provides package "example.com/x"`) {
		t.Fatalf("got %q", got)
	}

	if _, err := l.ResolveDir("."); err == nil || !strings.Contains(err.Error(), "outside a module") {
		t.Fatalf("got %v", err)
	}
}

func TestResolve_BrokenGoMod(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "this is not a go.mod\n")

	l := newLoaderAt(t, dir, map[string]string{"GOROOT": runtime.GOROOT()})

	if l.Package("io").Types["Reader"] == nil {
		t.Fatal("a broken go.mod must not affect the standard library")
	}

	if got := loadErr(l, "example.com/x"); !strings.Contains(got, "go.mod") {
		t.Fatalf("got %q", got)
	}
}

func TestResolve_ReplaceDirMissing(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/m\n\ngo 1.26\n\nrequire example.com/r v1.0.0\n\nreplace example.com/r => ./gone\n")

	l := newLoaderAt(t, dir, map[string]string{"GOROOT": runtime.GOROOT()})

	if got := loadErr(l, "example.com/r"); !strings.Contains(got, "replacement directory") {
		t.Fatalf("got %q", got)
	}
}

func TestResolve_VersionedReplaceOnlyMatchesItsVersion(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/m\n\ngo 1.26\n\nrequire example.com/r v1.0.0\n\nreplace example.com/r v0.9.0 => ./other\n")
	writeFile(t, dir, "other/r.go", "package r\n\ntype FromOther struct{}\n")

	cache := t.TempDir()
	writeFile(t, cache, "example.com/r@v1.0.0/r.go", "package r\n\ntype FromCache struct{}\n")

	l := newLoaderAt(t, dir, map[string]string{"GOROOT": runtime.GOROOT(), "GOMODCACHE": cache})

	if l.Package("example.com/r").Types["FromCache"] == nil {
		t.Fatal("a replace for v0.9.0 must not apply to v1.0.0")
	}
}

func TestResolve_ExactReplaceBeatsVersionless(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/m\n\ngo 1.26\n\nrequire example.com/r v1.0.0\n\n"+
		"replace example.com/r => ./any\n\nreplace example.com/r v1.0.0 => ./exact\n")
	writeFile(t, dir, "any/r.go", "package r\n\ntype Any struct{}\n")
	writeFile(t, dir, "exact/r.go", "package r\n\ntype Exact struct{}\n")

	l := newLoaderAt(t, dir, map[string]string{"GOROOT": runtime.GOROOT()})

	if l.Package("example.com/r").Types["Exact"] == nil {
		t.Fatal("the replace of the exact version must win")
	}
}

func TestResolve_NestedModuleInsideCacheModule(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	writeFile(t, dir, "go.mod", "module example.com/m\n\ngo 1.26\n\nrequire (\n\texample.com/a v1.0.0\n\texample.com/a/b v1.0.0\n)\n")

	cache := t.TempDir()
	writeFile(t, cache, "example.com/a@v1.0.0/a.go", "package a\n")
	writeFile(t, cache, "example.com/a/b@v1.0.0/b.go", "package b\n\ntype InNested struct{}\n")

	l := newLoaderAt(t, dir, map[string]string{"GOROOT": runtime.GOROOT(), "GOMODCACHE": cache})

	// The longer module path owns the package.
	if l.Package("example.com/a/b").Types["InNested"] == nil {
		t.Fatal("longest module path must win")
	}
}

func TestGoRoot_Order(t *testing.T) {
	fake := t.TempDir()
	writeFile(t, fake, "src/fakestd/fakestd.go", "package fakestd\n\ntype Marker struct{}\n")

	// 1. GOROOT wins over everything.
	l := newLoaderAt(t, t.TempDir(), map[string]string{"GOROOT": fake})
	if l.Package("fakestd").Types["Marker"] == nil {
		t.Fatal("GOROOT not honored")
	}

	// 2. Without GOROOT, the go binary on PATH locates it; it is only looked up, never run.
	root := t.TempDir()
	writeFile(t, root, "src/fakestd/fakestd.go", "package fakestd\n\ntype Marker struct{}\n")
	writeFile(t, root, "bin/go", "#!/bin/false\n")

	if err := os.Chmod(filepath.Join(root, "bin", "go"), 0o755); err != nil {
		t.Fatal(err)
	}

	link := t.TempDir()
	if err := os.Symlink(filepath.Join(root, "bin", "go"), filepath.Join(link, "go")); err != nil {
		t.Skipf("symlinks unsupported: %v", err)
	}

	t.Setenv("PATH", link)

	l = newLoaderAt(t, t.TempDir(), nil)
	if l.Package("fakestd").Types["Marker"] == nil {
		t.Fatal("GOROOT derived from the go binary (through a symlink) not used")
	}

	// 3. No GOROOT and no go on PATH: the toolchain this binary was built with.
	t.Setenv("PATH", "")

	l = newLoaderAt(t, t.TempDir(), nil)
	if l.Package("io").Types["Reader"] == nil {
		t.Fatal("runtime.GOROOT fallback not used")
	}
}

func TestGoEnvFile(t *testing.T) {
	t.Parallel()

	cache := abs(t, filepath.Join(modresDir, "modcache"))
	envFile := filepath.Join(t.TempDir(), "env")
	writeFile(t, filepath.Dir(envFile), "env", "# comment\nGOMODCACHE="+cache+"\r\nGOFLAGS=-mod=mod\nGOROOT="+runtime.GOROOT()+"\n")

	l, err := load.New(modresDir, load.WithGetenv(func(k string) string {
		if k == "GOENV" {
			return envFile
		}

		return ""
	}))
	if err != nil {
		t.Fatal(err)
	}

	if l.Package("example.com/cached").Types["Item"] == nil {
		t.Fatal("GOMODCACHE from the go env file not used")
	}

	// A process environment variable beats the file.
	l, err = load.New(modresDir, load.WithGetenv(func(k string) string {
		switch k {
		case "GOENV":
			return envFile
		case "GOMODCACHE":
			return t.TempDir()
		}

		return ""
	}))
	if err != nil {
		t.Fatal(err)
	}

	if got := loadErr(l, "example.com/cached"); !strings.Contains(got, "is not in the module cache") {
		t.Fatalf("environment must override the env file, got %q", got)
	}

	// GOFLAGS comes from the file as well.
	vl, err := load.New(modresVendor, load.WithGetenv(func(k string) string {
		switch k {
		case "GOENV":
			return envFile
		case "GOMODCACHE":
			return t.TempDir()
		}

		return ""
	}))
	if err != nil {
		t.Fatal(err)
	}

	if got := loadErr(vl, "example.com/dep"); !strings.Contains(got, "is not in the module cache") {
		t.Fatalf("GOFLAGS=-mod=mod from the env file should disable vendoring, got %q", got)
	}
}
