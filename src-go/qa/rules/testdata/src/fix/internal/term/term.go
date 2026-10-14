// Package term owns the home abbreviation.
package term

import "strings"

// AbbrevHome shows a path under home as ~/...
func AbbrevHome(path, home string) string {
	if rest, ok := strings.CutPrefix(path, home+"/"); ok {
		return "~/" + rest
	}
	return strings.Replace(path, home, "~", 1)
}
