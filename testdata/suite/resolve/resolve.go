package resolve

import "example.com/suite/model"

const Size = 4

const (
	base  = 2
	total = base*3 + Size
)

const (
	k0 = iota * 2
	k1
	k2
	k3
)

const (
	f0 = 1 << iota
	f1
	f2
)

type (
	id                = string
	ids               = []id
	user              = model.User
	handler           = func(u user) error
	set[T comparable] = map[T]struct{}
)

// Aliased mentions unexported aliases, nested and generic ones, of this package.
type Aliased interface {
	Get(id id) (user, error)
	List(ids ids) []user
	Each(h handler) error
	Seen(s set[id]) bool
	Chans(c chan id, d <-chan ids)
}

// Consts uses unexported constants, constant expressions and iota as array lengths.
type Consts interface {
	Arr(a [total]int, b [f2]byte) [k3]string
	Mixed(a [Size + base]int) [len("abc")]byte
	Conv(a [int(base) * 2]int) error
	Named(a [Size * 2]int) [k1 + f1]bool
}
