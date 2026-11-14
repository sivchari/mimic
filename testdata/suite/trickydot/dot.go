package trickydot

import . "example.com/suite/other"

// Dot: bare Thing is other.Thing through the dot import.
type Dot interface {
	Use(t Thing) Thing
}
