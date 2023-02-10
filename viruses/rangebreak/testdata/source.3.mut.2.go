//go:build testdata

package source

func main() {

	for _, s := range []string{} {
		println(s)
	}

	for _, i := range []int{} {
		break
		println(i + 1)

		for i := 0; i < 10; i++ {
			println(i)
		}

		for _, b := range []bool{} {
			println(b)
		}
	}

}
