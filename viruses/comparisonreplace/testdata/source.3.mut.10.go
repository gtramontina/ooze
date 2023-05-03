package source

func main() {
	_ = (1 == 1 || 2 == 2) && 3 != 4
	_ = 1 == 1 || (2 == 2 && 3 != 4)
	_ = 1 < 2 && 3 > 2 || false
	_ = true && 1 == 1
	_ = 1 == 1 && true
	_ = false || 1 == 1
	_ = 1 == 1 || false
}
