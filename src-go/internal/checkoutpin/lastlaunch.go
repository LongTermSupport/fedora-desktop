package checkoutpin

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path"
	"regexp"
	"slices"
	"strings"
	"time"
	"unicode/utf8"
)

// LastLaunch is the launcher's saved launch choices (Quick Launch), in its checkout: the token,
// the SSH keys forwarded and the network joined. A launch with nobody to ask takes them unasked,
// so a container that rewrote them would choose its next launch's keys and network (K5).
//
//   - K5. The launcher writes the file only through WriteLastLaunch, which records the digest of
//     the very bytes it writes, so a launch nobody attends takes only content the launcher
//     itself wrote or a person approved (ccy-teams approve last-launch). An attended launch
//     shows the choices and asks, as before, and its own write records them again.
const LastLaunch = ".claude/ccy/.last-launch.conf"

// LastLaunchPath is LastLaunch in checkout, as a message names it.
func LastLaunchPath(checkout string) string { return checkout + "/" + LastLaunch }

// KindLastLaunch approves LastLaunch.
const KindLastLaunch Kind = "last-launch"

// LastLaunchKeys are the keys the launcher writes, in its order.
var LastLaunchKeys = []string{
	lastLaunchFormat, "SAVED_CCY_VERSION", "SAVED_CCY_HASH", "LAST_TOKEN", "LAST_SSH_KEYS",
	"LAST_NETWORK", "LAST_TEAMS", "LAST_TEAM_ROLES", "LAST_LAUNCH_DATE",
}

const lastLaunchFormat = "SAVED_CONFIG_VERSION"

var (
	formatLine = regexp.MustCompile(`^` + lastLaunchFormat + `=([0-9]+)$`)
	quotedLine = regexp.MustCompile(`^([A-Z_]+)="([^"\\$` + "`" + `]*)"$`)
)

// Setting is one key of LastLaunch and its value.
type Setting struct{ Key, Value string }

// ParseLastLaunch reads LastLaunch strictly: `#` comment lines, SAVED_CONFIG_VERSION=<digits>
// and each other key of LastLaunchKeys once as KEY="value", the value free of a quote, a
// backslash, a `$`, a backtick and every control character, so it means the same text to bash
// as here; the last line ends in a newline, and the text is UTF-8.
func ParseLastLaunch(text []byte) ([]Setting, error) {
	if len(text) == 0 {
		return nil, nil
	}
	if text[len(text)-1] != '\n' {
		return nil, errors.New("it does not end in a newline (cut short?)")
	}
	var out []Setting
	for i, line := range bytes.Split(text[:len(text)-1], []byte("\n")) {
		s, err := lastLaunchLine(string(line))
		if err != nil {
			return nil, fmt.Errorf("line %d: %w", i+1, err)
		}
		if s.Key == "" {
			continue
		}
		if slices.ContainsFunc(out, func(o Setting) bool { return o.Key == s.Key }) {
			return nil, fmt.Errorf("line %d: %s is set twice", i+1, s.Key)
		}
		out = append(out, s)
	}
	return out, nil
}

// lastLaunchLine is one line's setting; no key for a comment.
func lastLaunchLine(line string) (Setting, error) {
	if !utf8.ValidString(line) {
		return Setting{}, errors.New("it is not UTF-8 text")
	}
	if unshowable(line) {
		return Setting{}, errors.New("it holds a control or format character (K6)")
	}
	if strings.HasPrefix(line, "#") {
		return Setting{}, nil
	}
	if m := formatLine.FindStringSubmatch(line); m != nil {
		return Setting{lastLaunchFormat, m[1]}, nil
	}
	m := quotedLine.FindStringSubmatch(line)
	if m == nil || m[1] == lastLaunchFormat {
		return Setting{}, fmt.Errorf("it is not KEY=\"value\" as ccy writes it: %q", line)
	}
	if !slices.Contains(LastLaunchKeys, m[1]) {
		return Setting{}, fmt.Errorf("%s is not a key ccy writes", m[1])
	}
	return Setting{m[1], m[2]}, nil
}

// LastLaunchDigest is LastLaunch's content and digest, and whether there is one (K4, K5).
func LastLaunchDigest(checkout string) (content []byte, digest string, present bool, err error) {
	return fileDigest(checkout, LastLaunch)
}

// WriteLastLaunch replaces LastLaunch in checkout with text, which must parse, and records its
// digest as approved (K5). The file is written beside itself and renamed into place, all
// through one os.Root of the checkout, so a link a container put in the file's place is
// replaced and a linked directory that leaves the checkout is refused.
func WriteLastLaunch(s Store, checkout string, text []byte, at time.Time) (err error) {
	if _, err := ParseLastLaunch(text); err != nil {
		return fmt.Errorf("refusing to write %s/%s: %w", checkout, LastLaunch, err)
	}
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return err
	}
	defer closeRoot(root, &err)
	var suffix [8]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		return err
	}
	tmp := LastLaunch + ".tmp." + hex.EncodeToString(suffix[:])
	if err := writeNew(root, tmp, text); err != nil {
		return errors.Join(fmt.Errorf("could not write %s/%s: %w", checkout, LastLaunch, err), removeIfThere(root, tmp))
	}
	if err := root.Rename(tmp, LastLaunch); err != nil {
		return errors.Join(fmt.Errorf("could not move %s/%s into place: %w", checkout, LastLaunch, err), removeIfThere(root, tmp))
	}
	sum := sha256.Sum256(text)
	return s.Record(checkout, KindLastLaunch, hex.EncodeToString(sum[:]), at)
}

// RemoveLastLaunch removes LastLaunch from checkout; one already gone is success.
func RemoveLastLaunch(checkout string) (err error) {
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return err
	}
	defer closeRoot(root, &err)
	return removeIfThere(root, LastLaunch)
}

// writeNew creates name exclusively, private, and writes text into it.
func writeNew(root *os.Root, name string, text []byte) error {
	f, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	_, err = f.Write(text)
	return errors.Join(err, f.Close())
}

func removeIfThere(root *os.Root, name string) error {
	if err := root.Remove(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return nil
}

// fileDigest is one pinned file's content and digest, and whether it is there.
func fileDigest(checkout, name string) (content []byte, digest string, present bool, err error) {
	root, err := os.OpenRoot(checkout)
	if err != nil {
		return nil, "", false, err
	}
	defer closeRoot(root, &err)
	content, present, err = readPinned(root, path.Clean(name))
	if err != nil || !present {
		return nil, "", false, err
	}
	sum := sha256.Sum256(content)
	return content, hex.EncodeToString(sum[:]), true, nil
}
