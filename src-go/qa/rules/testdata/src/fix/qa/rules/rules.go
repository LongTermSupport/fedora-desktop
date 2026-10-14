// Package rules is the rules package: its own source names the "~" it forbids elsewhere.
package rules

import "strings"

func tilde(p string) bool {
	return strings.HasPrefix(p, "~")
}
