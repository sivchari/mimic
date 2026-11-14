package trickydot

import "context"

type CtxPlain interface {
	Do2(ctx context.Context) error
}

// CtxBoth: Context (dot import) and context.Context are the same type with different texts.
type CtxBoth interface {
	CtxDot
	CtxPlain
}
