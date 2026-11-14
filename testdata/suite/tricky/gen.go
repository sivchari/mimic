package tricky

import "example.com/suite/other"

type GA[T any] interface {
	Get() T
}

// GB: the T in GA[string].Get (GA's T = string) and the T in Put (GB's T) have the same text, different types.
type GB[T any] interface {
	GA[string]
	Put(t T)
}

// GShadow has a type parameter named T while the package also declares type T.
type GShadow[T any] interface {
	Get() T
	Other(t other.Thing) error
}

type PkgT interface {
	GetT() T
}

// Mix: Get() returns GShadow's T; GetT() returns the package-level T.
type Mix interface {
	GShadow[int]
	PkgT
}

type GK[K comparable, V any] interface {
	GA[V]
	Put2(k K, v V) bool
}
