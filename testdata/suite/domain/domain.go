package domain

import (
	"context"
	"io"
	"time"

	"example.com/suite/model"
	"example.com/suite/other"
)

type Repo interface {
	Get(ctx context.Context, id string) (*model.User, error)
}

// Multi5 has 5 methods with differing arities, variadic, other-package types, alias, chan, func.
type Multi5 interface {
	Get(ctx context.Context, id string) (*model.User, error)
	Put(ctx context.Context, u *model.User) error
	List(ctx context.Context, filter other.Thing, opts ...other.Alias) ([]*model.User, int, error)
	Close()
	Watch(h func(*model.User) error, ch <-chan other.Thing) (chan<- int, func() error)
}

type Var interface {
	Do(prefix string, xs ...int) (n int, err error)
}

type MyInt = int

type Al interface {
	Do(a MyInt, b other.Alias, c other.Fn) MyInt
}

type unexp struct{ v int }

// U has an unexported type in an exported method.
type U interface {
	Use(u unexp) error
}

func MakeUnexp() unexp                          { return unexp{v: 7} }
func UseFn(f func(int) error) func(unexp) error { return func(u unexp) error { return f(u.v) } }

// Embeds: same-package interface + std interface + other-package interface.
type Embeds interface {
	Repo
	io.Writer
	other.Base
}

type RW interface {
	io.Reader
	Close() error
}

// Empty has a method with zero params and zero results.
type Empty interface {
	Close()
}

// Names has unnamed, blank and colliding parameter names. The methods are processed in name
// order, so ABefore comes before every method that imports a package named like its parameters.
type Names interface {
	A(int, string) error
	B(_ int, mock string, fn bool) (int, error)
	// No import of context, domain or sync is registered yet, so the names are kept.
	ABefore(context string, domain int, sync bool)
	// Unnamed context.Context and error.
	Ctx(context.Context, error) error
	// A name that is also an import name, and an explicit ctx.
	Imports(model model.User, other other.Thing, ctx context.Context) error
	// Whole-name initialisms.
	Initialisms(html string, id int, url string, userID int, ctx context.Context)
	// Blank names of the same type, and an explicit name that looks like a numbered one.
	Dups(_ int, _ int, _ int, _ string, _ string, n1 int)
	// Unnamed slice, array, map, chan, func, pointer, struct and interface.
	Kinds(_ []string, _ [2]int, _ map[string]int, _ chan int, _ func(), _ *int, _ *model.User, _ []*model.User, _ struct{}, _ interface{}, _ <-chan error)
	// Basic types, an alias, byte and any.
	Basics(_ bool, _ int, _ uint, _ float64, _ complex128, _ byte, _ rune, _ MyInt, _ any, _ []byte, _ []rune)
	// A name that is not an import yet, then types of imported packages.
	Pkgs(_ time.Time, _ time.Duration, _ *time.Time, _ []time.Duration, _ io.Reader)
	Vari(_ ...string)
	// An alias of another package is named "v", a defined type of that package after the type.
	Aliases(_ other.Alias, _ []other.Alias, _ other.Fn, _ []other.Thing)
	// A result variable takes the name nOut, which a parameter already has.
	Out(nOut int) (n int, err error)
	// An explicit name that the generated body also uses.
	Body(mock int, callInfo int, panic int, append int, NamesMockBodyCall int)
}

// GNames has unnamed parameters of a type parameter.
type GNames[T any] interface {
	Put(T, []T, *T, map[string]T) T
}

// GNamesString embeds GNames with a type argument, so the parameters are named after string.
type GNamesString interface {
	GNames[string]
}

type GRepo[T any] interface {
	Get(id int) (T, error)
	List(ids ...int) ([]T, error)
}

// GC has a constraint on its type parameter other than any.
type GC[K comparable, V any] interface {
	Put(k K, v V) (V, bool)
}
