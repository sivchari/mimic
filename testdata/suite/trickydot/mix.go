package trickydot

import "example.com/suite/third"

// DotMix: bare "Thing" in Dot (other.Thing, dot import) and bare "Thing" in third.Shower (third.Thing).
type DotMix interface {
	Dot
	third.Shower
}
