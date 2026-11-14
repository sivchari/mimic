# mimic

A mock generator for Go interfaces. It reads interfaces from source without type checking
and writes each one as a concrete, moq-style mock.

- Packages are found from local files only (GOROOT/src, `go.mod`, `go.work`, `vendor/` and the
  module cache); the `go` command is never started. Files are selected with `go/build` and parsed
  with `go/parser`. Nothing is type-checked, so generation is fast and does not depend on the
  package compiling.
- The type text of every method is copied into the output package with re-qualified imports.
  Unexported aliases and constants are resolved through the AST, so they never have to be named.
- The generated mock has no constructor and no dependency on `testing`.

## Install

With `go install`:

```bash
go install github.com/sivchari/mimic/cmd/mimic@latest
```

As a tool of a module (Go 1.24 or later), then run it with `go tool mimic`:

```bash
go get -tool github.com/sivchari/mimic/cmd/mimic@latest
```

Or download a binary for Linux, macOS or Windows (amd64 and arm64) from the
[releases](https://github.com/sivchari/mimic/releases) page.

## Generate

```bash
mimic gen [-o file] [-pkg name] [-skip-ensure] <importpath|./dir> <Iface>...
mimic version
```

```bash
# mocks for two interfaces of ./internal/domain into mocks/mocks_gen.go
mimic gen -o mocks/mocks_gen.go -pkg mocks ./internal/domain Repo Cache
```

From `go:generate` (the working directory is the package directory):

```go
//go:generate mimic gen -o repo_mock.go . Repo
```

Without `-o` the source is written to stdout. Without `-pkg` the package name is taken from the
Go files already in the output directory, then `$GOPACKAGE` when the output is in the working
directory, then `mocks`.

Every package-level name a generated file declares is derived from an interface name
(`RepoMock`, `RepoMockGetCall`), so several generated files can live in one package.

## Use

```go
m := &RepoMock{
	GetFunc: func(ctx context.Context, id string) (*User, error) {
		return &User{ID: id}, nil
	},
}

m.Get(ctx, "a")
calls := m.GetCalls() // []RepoMockGetCall{{Ctx: ctx, ID: "a"}}
```

`RepoMockGetCall` is an alias of an anonymous struct, exactly as in moq, so a func literal over the
struct type can be passed to a generic helper (a `Map[T, U any](s []T, f func(T) U)`) without a
named type breaking type inference.

The fields of a call record are named as in moq v0.6.0: the parameter name with its first letter
upper-cased (`userId` becomes `UserId`, a name that is an initialism such as `id` or `html`
becomes `ID` or `HTML`). An unnamed or blank parameter is named after its type (`context.Context`
becomes `ContextMoqParam`, `[]string` becomes `Strings`, `error` becomes `Err`), and duplicates
are numbered (`N1`, `N2`). The variable in the generated method can differ from moq where moq's
name would not compile in the generated body, for example a parameter named `mock`; the field
keeps moq's name.

Calls are recorded under a mutex, and `GetCalls` returns a copy, so a mock can be used from
several goroutines. Calling a method whose `Func` is nil panics with
`mimic: RepoMock.Get called, but GetFunc is nil`.

Every mock is followed by a compile-time check that it still implements the interface:

```go
// If this fails to compile, domain.Repo has changed: run "mimic gen".
var _ domain.Repo = (*RepoMock)(nil)
```

With `-skip-ensure` the check, and the import of the interface's package it needs, are left out.
Use it when the mock lives in a package that the interface's package imports, where the check
would be an import cycle. The package is still imported if a method signature names one of its
types.

For a generic interface the mock and its call records are generic too (`GRepoMock[T]`,
`GRepoMockGetCall[T]`); the generic alias needs Go 1.24 or later in the module that holds the mock.

## How packages are found

mimic reads the same files the `go` command would, without starting it:

- `go.mod` and `go.work` (`use` and `replace` directives, `GOWORK`) select the modules.
- `vendor/` is used when the `go` command would use it (`-mod=vendor` in `GOFLAGS`, or a `go`
  directive of 1.14 or later together with `vendor/modules.txt`).
- Everything else comes from the module cache (`GOMODCACHE`, `GOPATH`) and `GOROOT`.

Nothing is downloaded. If a required module is not in the module cache, mimic stops with an error
that names the module and tells you to run `go mod download`; a package missing from `vendor/`
tells you to run `go mod vendor`.

## What can be written

A mock lives in another package than the interface, so every type in a method signature must be
spelled from there.

| In the signature | Written as |
| --- | --- |
| exported type | `pkg.Type` |
| unexported alias (`type id = string`) | its target, recursively (`string`) |
| alias in an `internal/` package | its target, when the target is writable |
| unexported constant as an array length (`[sz]int`) | its value (`[2]int`) |
| constant expression such as `[Size * 2]int`, with `iota` | copied if every name is reachable, otherwise its value |

Anything else is refused with a one-line message that names the fix:

```
mimic: domain.Vault: domain.secret is unexported; generate the mock in package domain
mimic: lib.Pipe: example.com/x/lib/internal/types is internal; generate the mock inside example.com/x/lib
mimic: hcases.StructLit: struct literal has unexported field a; generate the mock in package hcases
```

Interfaces with unexported methods declared in another package cannot be implemented from
outside and are refused too.

## Detect stale mocks in CI

The header of a generated file records the interface (`// source: <importpath>.<Iface>`) and
the conformance check fails to compile when a method is added, removed or changed. To also catch
changes that still compile, regenerate and compare:

```bash
go generate ./... && git diff --exit-code
```

## Development

```bash
make test           # go test ./... -shuffle on -race
make lint           # gokui lint (go vet analyzers and modernize)
make fmt            # gokui fmt
make update-golden  # regenerate testdata/golden after an intended output change
make build          # build ./bin/mimic
```

[gokui](https://github.com/sivchari/gokui) is needed for `make lint` and `make fmt`:

```bash
go install github.com/sivchari/gokui/cmd/gokui@v0.1.2
```

Releases are cut by [tagpr](https://github.com/Songmu/tagpr): merging its release pull request
tags the commit and GoReleaser publishes the binaries.

## License

MIT
