package source

func main() {
	_ = (1 == 1 || 2 == 2) && 3 != 4
	_ = 1 == 1 || (2 == 2 && 3 != 4)
	_ = true && 3 > 2 || 2 != 1
	_ = true && 1 == 1
	_ = 1 == 1 && true
	_ = false || 1 == 1
	_ = 1 == 1 || false
}
