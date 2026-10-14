package badhome

import (
	"bytes"
	"fmt"
	"path/filepath"
	"strings"
)

// The evasions: the same abbreviation built without one of the five prefix functions.

func concat(p, home string) string {
	if rest, ok := strings.CutPrefix(p, home); ok {
		return "~" + rest // want `"~" built by hand`
	}
	return p
}

func concatSlash(rest string) string {
	return "~/" + rest + "/x" // want `"~/" built by hand`
}

func sprintf(rest string) string {
	return fmt.Sprintf("~%s", rest) // want `fmt.Sprintf with "~%s"`
}

func errorf(rest string) error {
	return fmt.Errorf("~/%s", rest) // want `fmt.Errorf with "~/%s"`
}

func builder(rest string) string {
	var b strings.Builder
	b.WriteString("~/") // want `WriteString with "~/"`
	b.WriteString(rest)
	return b.String()
}

func buffer(rest string) string {
	var b bytes.Buffer
	b.WriteByte('~') // want `WriteByte with "~"`
	b.WriteString(rest)
	return b.String()
}

func trimLeft(p string) string {
	return strings.TrimLeft(p, "~") // want `strings.TrimLeft with "~"`
}

func replacer(p string) string {
	return strings.NewReplacer("/h", "~").Replace(p) // want `strings.NewReplacer with "~"`
}

func join(rest string) string {
	return filepath.Join("~", rest) // want `filepath.Join with "~"`
}

// notHome has a tilde that does not begin the text, so it is not an abbreviation.
func notHome(a, b string) string {
	return a + "~" + b + fmt.Sprintf("%s~", a)
}
