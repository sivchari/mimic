package hcases

import "example.com/suite/hidden"

// Embeds2 embeds an interface from another package whose methods mention that package's unexported types.
type Embeds2 interface {
	hidden.Sec
	Extra() int
}

// StructLit: an anonymous struct type with an unexported field cannot be written outside this package.
type StructLit interface {
	S(x struct{ a int }) error
	Ok(x struct{ A int }) error
}

// IfaceLit: an anonymous interface type with an unexported method.
type IfaceLit interface {
	I(x interface{ priv() }) error
}

// PrivMethod has an unexported method: nothing outside the package can implement it.
type PrivMethod interface {
	priv()
	Pub()
}

type GenEmbed[T any] interface {
	hidden.GenSec[T]
	Extra(t T) int
}

type GenEmbedInt interface {
	hidden.GenSec[int]
}
