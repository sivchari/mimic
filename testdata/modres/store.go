// Package modres is a fixture whose dependencies come from a fake module cache, a local replace
// and module replaces.
package modres

import (
	"example.com/Upper"
	"example.com/cached"
	"example.com/localrep"
)

// Store takes a type that only exists in the module cache.
type Store interface {
	Put(c cached.Item) error
	Get() (upper.Doc, error)
	Local(l localrep.Piece)
}
