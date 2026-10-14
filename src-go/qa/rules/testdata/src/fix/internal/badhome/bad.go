// Package badhome abbreviates home by hand.
package badhome

import "strings"

// A bare ~ prefix replace turned /home/<user>X into ~X.
func replace(p, home string) string {
	return strings.Replace(p, home, "~", 1) // want `strings.Replace with "~"`
}

func replaceAll(p, home string) string {
	return strings.ReplaceAll(p, home, "~") // want `strings.ReplaceAll with "~"`
}

func trim(p string) string {
	return strings.TrimPrefix(p, "~") // want `strings.TrimPrefix with "~"`
}

func has(p string) bool {
	return strings.HasPrefix(p, "~") // want `strings.HasPrefix with "~"`
}

func cut(p string) (string, bool) {
	return strings.CutPrefix(p, "~") // want `strings.CutPrefix with "~"`
}

func clean(p string) string {
	return strings.Replace(p, "/a", "/b", 1)
}
