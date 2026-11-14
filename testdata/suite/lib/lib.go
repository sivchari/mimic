package lib

import "example.com/suite/lib/internal/types"

// Svc reaches public types and a constant only through an internal package.
type Svc interface {
	Get(ref types.Ref) (types.Refs, error)
	Fixed(a [types.Size]int) error
}

// Pipe returns a defined type of the internal package.
type Pipe interface {
	Open() types.Conn
}
