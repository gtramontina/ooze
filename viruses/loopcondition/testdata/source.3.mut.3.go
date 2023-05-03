package source

import "fmt"

func main() {
	k := 0

	for k < 100 {
		k++
	}

	println(k)

	for i := 0; i < 10; i++ {
		println(i)
	}

	for _, s := range []string{} {
		for j := 0; 0 != 0; j++ {
			println(fmt.Sprintf("%s-%d", s, j))
		}

		for false {
			println("never here")
		}
	}
}
