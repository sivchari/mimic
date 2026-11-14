package tricky

import (
	"example.com/suite/other"
	"example.com/suite/pkgdiff"
	"example.com/suite/third"
)

// AliasClash: m.User in A1 (model) and m.User in A2 (other) have the same text but are different types.
type AliasClash interface {
	A1
	A2
}

// Unq: other.Finder.Find returns other.User (bare "User" in package other); Mine returns tricky.User.
type Unq interface {
	other.Finder
	Mine(u User) User
}

// Cross: model.User reached through alias m (A1) and alias mdl (third.UserGetter) is the same type.
type Cross interface {
	A1
	third.UserGetter
}

// PkgDiff imports a package whose name (realname) differs from the last path element (pkgdiff).
type PkgDiff interface {
	Pd(x realname.X) error
}

// SameText: identical text, identical type, repeated in many places.
type SameText interface {
	A(x *User, y []User) (*User, error)
	B(x *User) error
	C(y []User) []User
}

// Cross2: alias m means example.com/suite/model in A1 and example.com/suite/other in third.OtherUser; the generated imports must not clash.
type Cross2 interface {
	A1
	third.OtherUser
}
