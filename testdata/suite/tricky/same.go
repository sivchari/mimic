package tricky

import (
	"io"

	"example.com/suite/other"
)

type MyInt = int

// Aliases: different source text for the same type; each method must still be mocked as written.
type Aliases interface {
	A(x MyInt) error
	B(x int) error
	C(x any) error
	D(x interface{}) error
	E(x byte) error
	F(x uint8) error
	G(x other.Alias) error
	H(x other.Thing) error
	I(x []MyInt) error
	J(x []int) error
}

type RWC interface {
	io.ReadWriteCloser
}

type WithErr interface {
	error
	Do()
}
