package setuprclone

import (
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"unicode"
)

const maxNameLen = 64

var nameRule = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

// unsafeChars break the unit's ExecStart (unquoted, % a specifier), the push helper (quoted)
// or Ansible's templating (E15).
const unsafeChars = `'"\$` + "`" + `%{}`

// checkName is a mount name the play can build a unit, a push helper and a bookmark from (E15).
func checkName(name string, others []mount) error {
	if len(name) > maxNameLen || !nameRule.MatchString(name) {
		return fmt.Errorf("a mount name is lower-case letters, digits, '-' and '_', starting with a letter or digit, at most %d characters", maxNameLen)
	}
	for _, m := range others {
		if m.name == name {
			return fmt.Errorf("another mount is already named %s", name)
		}
	}
	return nil
}

// defaultName is the remote's name made a mount name (E15).
func defaultName(remote string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(remote) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
			dash = false
			continue
		}
		if !dash {
			b.WriteByte('-')
			dash = true
		}
	}
	name := strings.Trim(b.String(), "-_")
	if len(name) > maxNameLen {
		name = strings.Trim(name[:maxNameLen], "-_")
	}
	if name == "" {
		return "mount"
	}
	return name
}

// checkMountpoint is an absolute, clean directory apart from every other mount's (E15).
func checkMountpoint(path string, others []mount) error {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || path == "/" {
		return errors.New("a mountpoint is an absolute path, written without a trailing '/', '//' or '..', and not /")
	}
	if err := safeText(path); err != nil {
		return err
	}
	for _, m := range others {
		if m.mountpoint == path || within(path, m.mountpoint) || within(m.mountpoint, path) {
			return fmt.Errorf("%s overlaps the mountpoint of %s (%s)", path, m.name, m.mountpoint)
		}
	}
	return nil
}

func within(path, dir string) bool { return strings.HasPrefix(path, dir+"/") }

// checkRemote is a remote spec the play can write into the unit and the push helper (E15).
func checkRemote(spec string) error { return safeText(spec) }

func safeText(s string) error {
	for _, r := range s {
		if unicode.IsSpace(r) || unicode.IsControl(r) || strings.ContainsRune(unsafeChars, r) {
			return fmt.Errorf("%q holds %q: no spaces, control characters or any of %s, which the play's unit file, push helper or templating would break on", s, r, unsafeChars)
		}
	}
	return nil
}
