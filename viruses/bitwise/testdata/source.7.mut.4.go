//go:build testdata

package source

func main() {
	i := 10

	i & 1
	i | 1
	i ^ 1
	i & 1
	i << 1
	1 >> 1

	1 + 1
}
