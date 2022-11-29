package oozetesting

import "regexp"

func Source(doc string) []byte {
	return []byte(regexp.MustCompile(`(?m)^\s*\|`).ReplaceAllString(doc, ""))
}
