package namesx

import (
	"context"
	"sync"
	"testing"

	"example.com/suite/model"
	"example.com/suite/other"
)

// Shadow has parameters named like imported packages and predeclared types.
type Shadow interface {
	Do(model model.User, context context.Context) (string, error)
	Err(error error, string string) (int, error)
	Pkg(other other.Thing, sync int, testing bool) error
	Dup(a int, A int) error
	Mockish(mock int, callInfo int, calls int) error
}

type Chans interface {
	Cc(c chan (<-chan int)) <-chan <-chan int
	Dir(a chan<- int, b <-chan int, c chan int) (chan<- int, <-chan int)
}

type Tags interface {
	Tag(x struct {
		A int `json:"a"`
		B string
	}) error
	Fn(f func(a, b int) (c, d string), g func(...int)) error
	Lit(i interface{ M(int) string }) error
}

// Stdlib is a mock of types from the standard library that mix sync/testing names.
type Stdlib interface {
	Use(m *sync.Mutex, t testing.TB) error
}
