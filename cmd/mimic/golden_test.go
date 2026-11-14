package main

import (
	"flag"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

var update = flag.Bool("update", false, "rewrite the golden files in testdata/golden")

const goldenDir = "../../testdata/golden"

// TestGolden compares generated mocks with the files in testdata/golden. Run with -update to
// rewrite them after an intended change of the generated shape.
func TestGolden(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		path  string
		names []string
	}{
		{"repo", "example.com/suite/domain", []string{"Repo"}},
		{"multi5", "example.com/suite/domain", []string{"Multi5"}},
		{"variadic", "example.com/suite/domain", []string{"Var"}},
		{"embeds", "example.com/suite/domain", []string{"Embeds"}},
		{"names", "example.com/suite/domain", []string{"Names"}},
		{"generic", "example.com/suite/domain", []string{"GRepo"}},
		{"constraint", "example.com/suite/domain", []string{"GC"}},
		{"aliases_resolved", "example.com/suite/resolve", []string{"Aliased"}},
		{"constants_resolved", "example.com/suite/resolve", []string{"Consts"}},
		{"internal_alias", "example.com/suite/lib", []string{"Svc"}},
		{"aliases", "example.com/suite/tricky", []string{"AliasClash"}},
		{"two_in_one_file", "example.com/suite/domain", []string{"Repo", "Var"}},
	}

	root := copyTree(t, suiteDir)

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			out := filepath.Join("golden", tt.name+".go")
			args := append([]string{"gen", "-o", out, "-pkg", "mocks", tt.path}, tt.names...)

			if res := runMimic(root, args...); res.code != 0 {
				t.Fatalf("gen failed: %s", res.stderr)
			}

			got := readFile(t, root, out)
			golden := filepath.Join(goldenDir, tt.name+".go.golden")

			if *update {
				if err := os.WriteFile(golden, []byte(got), 0o600); err != nil {
					t.Fatal(err)
				}

				return
			}

			want, err := os.ReadFile(filepath.Clean(golden))
			if err != nil {
				t.Fatalf("%v (run with -update to create it)", err)
			}

			if got != string(want) {
				t.Errorf("generated output differs from %s (run with -update if intended):\n%s", golden, firstDiff(string(want), got))
			}
		})
	}
}

// firstDiff describes the first line at which want and got differ.
func firstDiff(want, got string) string {
	wl, gl := strings.Split(want, "\n"), strings.Split(got, "\n")

	for i := range max(len(wl), len(gl)) {
		var w, g string
		if i < len(wl) {
			w = wl[i]
		}

		if i < len(gl) {
			g = gl[i]
		}

		if w != g {
			return "line " + strconv.Itoa(i+1) + ":\n  want: " + w + "\n  got:  " + g
		}
	}

	return "no difference"
}
