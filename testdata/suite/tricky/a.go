package tricky

import m "example.com/suite/model"

// User is a package-local type whose bare name collides with other.User.
type User struct{ Local int }

// T is a package-level type whose bare name collides with a type parameter named T.
type T struct{ Local int }

// A1 uses alias m for example.com/suite/model.
type A1 interface {
	Get(id string) (m.User, error)
}
