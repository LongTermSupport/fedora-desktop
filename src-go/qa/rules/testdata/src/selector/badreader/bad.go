// Package badreader reads a recorded launch's conversation selectors outside the registry's
// parser, in every spelling the rule holds against.
package badreader

import (
	"slices"
	"strings"

	"selector/internal/registry"
)

const flagResume = "--resume"

// Continued compares by == and !=.
func Continued(rec registry.Record) bool {
	for _, w := range rec.Args {
		if w == "--continue" || "-c" == w { // want `claude's conversation selectors are read in registry's selectorOf alone` `claude's conversation selectors are read in registry's selectorOf alone`
			return true
		}
		if w != "--session-id" { // want `claude's conversation selectors are read in registry's selectorOf alone`
			continue
		}
	}
	return false
}

// Tagged reads by a tagged switch and a named constant.
func Tagged(rec registry.Record) string {
	for i, w := range rec.Args {
		switch w {
		case flagResume, "-r": // want `claude's conversation selectors are read in registry's selectorOf alone` `claude's conversation selectors are read in registry's selectorOf alone`
			if i+1 < len(rec.Args) {
				return rec.Args[i+1]
			}
		}
	}
	return ""
}

// Called reads by the strings and slices helpers.
func Called(rec registry.Record) (string, bool) {
	if slices.Contains(rec.Args, "--continue") { // want `claude's conversation selectors are read in registry's selectorOf alone`
		return "", true
	}
	for _, w := range rec.Args {
		if id, ok := strings.CutPrefix(w, "--session-id="); ok { // want `claude's conversation selectors are read in registry's selectorOf alone`
			return id, false
		}
	}
	return "", false
}

// Built writes a selector, which is not reading one, and compares an unrelated word.
func Built(rec registry.Record) []string {
	if len(rec.Args) > 0 && rec.Args[0] == "--token" {
		return nil
	}
	return append(rec.Args, flagResume, "x")
}
