//go:build testdata

package source

func main() {
	for {
		break
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
		continue
	}
}
