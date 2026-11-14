package namesx

import sync "example.com/suite/other"

// SyncAlias imports a package under the alias "sync", which the generated file also needs for the real sync package.
type SyncAlias interface {
	X(t sync.Thing) error
}
