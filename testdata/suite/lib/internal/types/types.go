package types

import "example.com/suite/model"

// Ref is an alias of a public type.
type Ref = model.User

// Refs is an alias of an alias of a public type.
type Refs = map[model.UserID]*Ref

// Size is an exported constant of an internal package.
const Size = 3

// Conn is a defined type: it cannot be rewritten away.
type Conn struct{}
