package tricky

import m "example.com/suite/other"

// A2 uses the SAME alias name m, but for example.com/suite/other.
type A2 interface {
	Get2(id string) (m.User, error)
}
