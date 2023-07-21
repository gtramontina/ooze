//!go:build testdata

package source

func hello0() string {
	x := "hello"
	return "/" + x
}

func hello1() string {
	x := "hello"
	return x + "/"
}

func hello3() string {
	x := "hello"
	y := []byte("/")
	return x + string(y)
}

// FIXME This still fails
// func hello2() string {
// 	x := "hello"
// 	y := "/"
// 	return x + y
// }

// FIXME This still fails
// func toString(b []byte) string {
// 	return string(x)
// }

// func hello4() string {
// 	x := "hello"
// 	y := []byte{"/"}
// 	return hello + toString(y)
// }
