// Command mimic generates mocks for Go interfaces.
//
//	mimic gen [-o file] [-pkg name] [-skip-ensure] <importpath|./dir> <Iface>...
//	mimic version
package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/sivchari/mimic"
)

const (
	exitOK    = 0
	exitError = 1
)

const usage = `usage:
  mimic gen [-o file] [-pkg name] [-skip-ensure] <importpath|./dir> <Iface>...
  mimic version
`

// app carries the process environment so that tests can run the CLI in-process.
type app struct {
	dir    string // working directory; "" for the process's
	stdout io.Writer
	stderr io.Writer
	getenv func(string) string
}

func main() {
	a := &app{stdout: os.Stdout, stderr: os.Stderr, getenv: os.Getenv}
	os.Exit(a.run(os.Args[1:]))
}

func (a *app) run(args []string) int {
	if len(args) == 0 {
		fmt.Fprint(a.stderr, usage)

		return exitError
	}

	switch args[0] {
	case "gen":
		return a.gen(args[1:])
	case "version":
		fmt.Fprintln(a.stdout, mimic.Version)

		return exitOK
	case "help", "-h", "-help", "--help":
		fmt.Fprint(a.stdout, usage)

		return exitOK
	}

	fmt.Fprintf(a.stderr, "mimic: unknown subcommand %q\n%s", args[0], usage)

	return exitError
}

// fail reports an error and returns the error exit code.
func (a *app) fail(format string, args ...any) int {
	fmt.Fprintf(a.stderr, "mimic: "+format+"\n", args...)

	return exitError
}

// abs resolves p against the working directory.
func (a *app) abs(p string) string {
	if p == "" || filepath.IsAbs(p) || a.dir == "" {
		return p
	}

	return filepath.Join(a.dir, p)
}
