//go:build testdata

package source

func main() {
	k := 0

	for k < 100 {
		k++
	}

	println(k)
}
