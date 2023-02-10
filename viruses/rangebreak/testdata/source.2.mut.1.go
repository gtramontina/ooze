//go:build testdata

package source

func main() {

	for _, s := range []string{} {
		break
		println(s)
	}

	for _, i := range []int{} {
		println(i + 1)
	}

}
