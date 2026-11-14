package iface_test

import (
	"go/ast"
	"slices"
	"strings"
	"testing"

	"github.com/sivchari/mimic/internal/iface"
	"github.com/sivchari/mimic/internal/load"
)

const suite = "../../testdata/suite"

func loadIface(t *testing.T, l *load.Loader, path, name string) *iface.Interface {
	t.Helper()

	it, err := iface.Load(l, path, name)
	if err != nil {
		t.Fatalf("%s.%s: %v", path, name, err)
	}

	return it
}

func methodNames(it *iface.Interface) []string {
	names := make([]string, len(it.Methods))
	for i, m := range it.Methods {
		names[i] = m.Name
	}

	return names
}

func TestLoad_Methods(t *testing.T) {
	t.Parallel()

	l, err := load.New(suite)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path, name string
		methods    []string
	}{
		{"example.com/suite/domain", "Repo", []string{"Get"}},
		// own, std and other-package embeddings, flattened and sorted
		{"example.com/suite/domain", "Embeds", []string{"Get", "Ping", "Write"}},
		// an embedded error contributes Error() string
		{"example.com/suite/tricky", "WithErr", []string{"Do", "Error"}},
		// the diamond: Get comes from A1 through two routes
		{"example.com/suite/tricky", "Cross", []string{"Get", "Fetch"}},
		{"example.com/suite/tricky", "GB", []string{"Get", "Put"}},
		{"example.com/suite/hcases", "GenEmbedInt", []string{"Boxed", "Plain", "Use"}},
		{"io", "ReadWriteCloser", []string{"Close", "Read", "Write"}},
	}

	for _, tt := range tests {
		it := loadIface(t, l, tt.path, tt.name)

		got := methodNames(it)
		want := slices.Sorted(slices.Values(tt.methods))

		if !slices.Equal(got, want) {
			t.Errorf("%s.%s methods = %v, want %v", tt.path, tt.name, got, want)
		}
	}
}

func TestLoad_Signature(t *testing.T) {
	t.Parallel()

	l, err := load.New(suite)
	if err != nil {
		t.Fatal(err)
	}

	m := loadIface(t, l, "example.com/suite/domain", "Multi5").Methods

	var list *iface.Method

	for _, x := range m {
		if x.Name == "List" {
			list = x
		}
	}

	if !list.Variadic || len(list.Params) != 3 || len(list.Results) != 3 {
		t.Fatalf("List: variadic=%v params=%d results=%d", list.Variadic, len(list.Params), len(list.Results))
	}

	if got := list.Params[2].Canon; got != "example.com/suite/other.Alias" {
		t.Errorf("variadic element canon = %q", got)
	}

	names := loadIface(t, l, "example.com/suite/domain", "Names")

	for _, x := range names.Methods {
		if x.Name != "B" {
			continue
		}

		if got := x.Sig(); !strings.HasPrefix(got, "B(a0 int, mock string, fn bool)") {
			t.Errorf("blank parameter should be named a0: %s", got)
		}
	}
}

func TestLoad_TypeParams(t *testing.T) {
	t.Parallel()

	l, err := load.New(suite)
	if err != nil {
		t.Fatal(err)
	}

	it := loadIface(t, l, "example.com/suite/domain", "GC")
	if len(it.TParams) != 2 || it.TParams[0].Name != "K" || it.TParams[1].Name != "V" {
		t.Fatalf("type parameters: %+v", it.TParams)
	}

	if got := it.TParams[0].Constraint.(*ast.Ident).Name; got != "comparable" {
		t.Fatalf("constraint of K = %s", got)
	}
}

func TestLoad_Errors(t *testing.T) {
	t.Parallel()

	l, err := load.New(suite)
	if err != nil {
		t.Fatal(err)
	}

	tests := []struct {
		path, name, want string
	}{
		{"example.com/suite/domain", "Missing", "domain.Missing not found"},
		{"example.com/suite/missing", "X", "cannot find package"},
		{"time", "Month", "time.Month is not an interface"},
		{"html/template", "FuncMap", "template.FuncMap is not an interface"},
		{"example.com/suite/domain", "MyInt", "domain.MyInt is not an interface"},
	}

	for _, tt := range tests {
		_, err := iface.Load(l, tt.path, tt.name)
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s.%s: got %v, want %q", tt.path, tt.name, err, tt.want)
		}
	}
}
