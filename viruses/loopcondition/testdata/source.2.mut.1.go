//go:build testdata

package source

func main() {
	k := 0

	for 0 != 0 {
		k++
	}

	println(k)

	for i := 0; i < 10; i++ {
		println(i)
	}
}
