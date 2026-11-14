package main

import (
	"errors"
	"flag"
	"fmt"
	"go/build"
	"os"
	"path/filepath"
	"strings"

	"github.com/sivchari/mimic/internal/emit"
	"github.com/sivchari/mimic/internal/iface"
	"github.com/sivchari/mimic/internal/load"
)

const defaultPackage = "mocks"

// gen implements `mimic gen [-o file] [-pkg name] [-skip-ensure] <importpath|./dir> <Iface>...`.
func (a *app) gen(args []string) int {
	fs := flag.NewFlagSet("gen", flag.ContinueOnError)
	fs.SetOutput(a.stderr)

	out := fs.String("o", "", "output file (default: standard output)")
	pkg := fs.String("pkg", "", "package name of the generated file")
	skipEnsure := fs.Bool("skip-ensure", false,
		"omit the check that each mock implements its interface; use it when the mock lives in a package that the interface's package imports")

	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return exitOK
		}

		return exitError
	}

	if fs.NArg() < 2 {
		return a.fail("gen: need <importpath|./dir> and at least one <Iface>")
	}

	l, err := load.New(a.dir, load.WithGetenv(a.getenv))
	if err != nil {
		return a.fail("%v", err)
	}

	path := fs.Arg(0)
	if isDirArg(path) {
		if path, err = l.ResolveDir(path); err != nil {
			return a.fail("%v", err)
		}
	}

	its := make([]*iface.Interface, 0, fs.NArg()-1)

	for _, name := range fs.Args()[1:] {
		it, err := iface.Load(l, path, name)
		if err != nil {
			return a.fail("%v", err)
		}

		its = append(its, it)
	}

	outFile := a.abs(*out)
	opts := emit.Options{Package: a.packageName(*pkg, outFile), SkipEnsure: *skipEnsure}

	if outFile != "" {
		opts.OutPath = load.ModulePath(outFile)
	}

	src, err := emit.Generate(l, opts, its)
	if err != nil {
		return a.fail("%v", err)
	}

	if outFile == "" {
		_, err = a.stdout.Write(src)
	} else {
		err = writeOutput(outFile, src)
	}

	if err != nil {
		return a.fail("%v", err)
	}

	return exitOK
}

// isDirArg reports whether a package argument names a directory rather than an import path.
func isDirArg(s string) bool {
	return s == "." || s == ".." || strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") || filepath.IsAbs(s)
}

// packageName picks the package clause of the generated file: the -pkg flag, else the package
// already in the output directory, else $GOPACKAGE (set by go generate) when the output is in the
// working directory, else "mocks".
func (a *app) packageName(flagValue, outFile string) string {
	if flagValue != "" {
		return flagValue
	}

	if outFile == "" {
		return defaultPackage
	}

	outDir := filepath.Dir(outFile)
	// An error (no Go files, mixed packages) still reports the name when one was found.
	if bp, _ := build.ImportDir(outDir, 0); bp != nil && bp.Name != "" {
		return bp.Name
	}

	if gp := a.getenv("GOPACKAGE"); gp != "" && sameDir(outDir, a.dir) {
		return gp
	}

	return defaultPackage
}

func sameDir(a, b string) bool {
	absA, errA := filepath.Abs(a)
	absB, errB := filepath.Abs(b)

	return errA == nil && errB == nil && absA == absB
}

// writeOutput writes src to path, creating the directory if needed.
func writeOutput(path string, src []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create output directory: %w", err)
	}

	if err := os.WriteFile(path, src, 0o644); err != nil {
		return fmt.Errorf("write output: %w", err)
	}

	return nil
}
