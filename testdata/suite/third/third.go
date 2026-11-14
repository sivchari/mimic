package third

import mdl "example.com/suite/model"

type Thing struct{ S string }

// Shower mentions Thing by bare name; in this package it is third.Thing.
type Shower interface {
	Show(t Thing) Thing
}

// UserGetter reaches model.User through an alias that differs from the one used elsewhere.
type UserGetter interface {
	Fetch() (mdl.User, error)
}
