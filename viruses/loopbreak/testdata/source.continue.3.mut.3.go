//go:build testdata

package source

func main() {
	for {
		continue
	}
	for {
		var _ = 1
	}
	for {
		continue
	}
	for {
		var _ = 2
	}
	for {
		break
	}
}
