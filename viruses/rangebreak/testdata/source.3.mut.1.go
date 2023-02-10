//go:build testdata

package source

func main() {

	for _, s := range []string{} {
		break
		println(s)
	}

	for _, i := range []int{} {
		println(i + 1)

		for i := 0; i < 10; i++ {
			println(i)
		}

		for _, b := range []bool{} {
			println(b)
		}

	}

}
