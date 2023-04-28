//go:build testdata

package testdata

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
		break
	}
}
