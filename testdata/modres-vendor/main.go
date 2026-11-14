package modresv

import "example.com/dep"

type User interface {
	Use(d dep.Thing)
}
