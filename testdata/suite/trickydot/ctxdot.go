package trickydot

import . "context"

type CtxDot interface {
	Do(ctx Context) error
}
