package consts

const Size = 4
const sz = 2

type Arr interface {
	A(a [Size]int, b [Size * 2]byte) [Size + 1]string
	B(a [sz]int) error
}
