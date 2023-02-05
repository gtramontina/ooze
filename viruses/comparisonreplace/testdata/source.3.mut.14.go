//go:build testdata

package source

func main() {
	a := (1 == 1 || 2 == 2) && 3 != 4
	b := 1 == 1 || (2 == 2 && 3 != 4)
	c := 1 < 2 && 3 > 2 || 2 != 1
	d := true && 1 == 1
	e := true && true
	f := false || 1 == 1
	g := 1 == 1 || false
}
