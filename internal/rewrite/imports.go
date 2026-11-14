package rewrite

import (
	"fmt"
	"go/token"
	"path"
	"sort"
	"strings"

	"github.com/sivchari/mimic/internal/canon"
	"github.com/sivchari/mimic/internal/load"
)

// Imports is the import table of one generated file. Names are assigned first come, first
// served; a clash with a different path gets a numeric suffix.
type Imports struct {
	byPath map[string]string
	byName map[string]string
	used   map[string]bool
}

// NewImports returns an import table in which sync is reserved for the generated code itself.
func NewImports() *Imports {
	im := &Imports{byPath: map[string]string{}, byName: map[string]string{}, used: map[string]bool{}}
	im.reserve("sync", "sync")

	return im
}

func (im *Imports) reserve(p, name string) {
	im.byPath[p] = name
	im.byName[name] = p
}

// Name returns the file-level name of the package with import path p, preferring pref, and marks
// the import as used.
func (im *Imports) Name(p, pref string) string {
	im.used[p] = true

	if n, ok := im.byPath[p]; ok {
		return n
	}

	n := pref

	for i := 2; ; i++ {
		_, taken := im.byName[n]
		if !taken && !canon.IsPredeclared(n) && !token.IsKeyword(n) {
			break
		}

		n = fmt.Sprintf("%s%d", pref, i)
	}

	im.reserve(p, n)

	return n
}

// Use marks an already reserved import as used.
func (im *Imports) Use(p string) { im.used[p] = true }

// Taken reports whether name is a file-level import name.
func (im *Imports) Taken(name string) bool {
	_, ok := im.byName[name]

	return ok
}

// Registered reports whether name is the file-level name of an import that the file uses so far.
// The reserved sync is not registered until something marks it as used.
func (im *Imports) Registered(name string) bool {
	p, ok := im.byName[name]

	return ok && im.used[p]
}

// Lines renders the used imports as the entries of an import block: standard library packages
// first, then the rest, each group sorted by import path.
func (im *Imports) Lines() string {
	ps := make([]string, 0, len(im.used))
	for p := range im.used {
		ps = append(ps, p)
	}

	sort.Slice(ps, func(i, j int) bool {
		si, sj := load.IsStd(ps[i]), load.IsStd(ps[j])
		if si != sj {
			return si
		}

		return ps[i] < ps[j]
	})

	var b strings.Builder

	for i, p := range ps {
		if i > 0 && load.IsStd(ps[i-1]) && !load.IsStd(p) {
			b.WriteString("\n")
		}

		if n := im.byPath[p]; n == path.Base(p) {
			fmt.Fprintf(&b, "\t%q\n", p)
		} else {
			fmt.Fprintf(&b, "\t%s %q\n", n, p)
		}
	}

	return b.String()
}
