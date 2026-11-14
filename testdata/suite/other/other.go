package other

type Thing struct{ N int }
type Alias = Thing
type Fn func() error

// Base is embedded from a different package.
type Base interface {
	Ping(n int) bool
}

// User exists to collide with other packages' User by bare name.
type User struct{ N int }

// Finder is embedded from another package; Find's result is other.User (bare name User inside this package).
type Finder interface {
	Find(id int) User
}
