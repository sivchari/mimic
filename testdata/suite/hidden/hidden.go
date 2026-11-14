package hidden

type secret struct{ V int }
type box[T any] struct{ V T }

const nn = 3

// Sec has unexported types of this package in exported methods.
type Sec interface {
	UseSecret(s secret) error
	Secrets() []secret
	ByName(m map[string]*secret) (secret, bool)
	Boxed(b box[int]) box[string]
	Fixed(a [nn]int) [nn]secret
	Fn(f func(secret) error) func() secret
	Ch(c chan secret) <-chan secret
	Visible(x int) string
}

// GenSec combines an interface type parameter with unexported types.
type GenSec[T any] interface {
	Use(s secret, t T) T
	Boxed(b box[T]) []box[T]
	Plain(t T) error
}

// Sized uses an unexported constant as an array length and nothing else that is unexported.
type Sized interface {
	Fixed(a [nn]int) [nn]string
}

func NewSecret(v int) secret { return secret{V: v} }
func SecretV(s secret) int   { return s.V }

// WrapUse adapts a func over ints to the unexported parameter type.
func WrapUse(f func(int) error) func(secret) error { return func(s secret) error { return f(s.V) } }
func WrapSecrets(f func() []int) func() []secret {
	return func() []secret {
		var out []secret
		for _, v := range f() {
			out = append(out, secret{V: v})
		}
		return out
	}
}
func WrapGenUse[T any](f func(int, T) T) func(secret, T) T {
	return func(s secret, t T) T { return f(s.V, t) }
}
