package ccylaunch

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// F16, --read-launch-config FILE: the bash launcher's saved launch configuration
// (.claude/ccy/.last-launch.conf), read strictly and handed back one `KEY<TAB>value` line per
// key, in the file's order, for the bash to take with `read` (no source, no eval). The file
// is the one the launcher writes at the end of a launch: `#` comment lines, then
// SAVED_CONFIG_VERSION=<digits> and each other key as KEY="value", the value free of a
// quote, a backslash, a `$`, a backtick and every control character, so it means the same
// text to bash as it does here. Any other line, a key outside launchConfigKeys, a key twice, a
// last line with no newline, or text that is not UTF-8 is malformed. A file that cannot be
// read or is malformed is exit 1, one stderr line naming the path and the cause, and nothing
// on stdout; the reader never changes the file. Which keys a format needs, and what a format
// other than this ccy's means, is the launcher's to decide. The flag is answered under
// CCY_LEGACY too: the bash launcher asks it, so handing it over would ask the bash itself.

const (
	launchConfigFlag  = "--read-launch-config"
	launchConfigUsage = "usage: claude-yolo " + launchConfigFlag + " FILE\n"
	formatKey         = "SAVED_CONFIG_VERSION"
)

// launchConfigKeys are the keys the launcher writes.
var launchConfigKeys = []string{
	formatKey, "SAVED_CCY_VERSION", "SAVED_CCY_HASH", "LAST_TOKEN", "LAST_SSH_KEYS",
	"LAST_NETWORK", "LAST_TEAMS", "LAST_LAUNCH_DATE",
}

var (
	formatLine = regexp.MustCompile(`^` + formatKey + `=([0-9]+)$`)
	quotedLine = regexp.MustCompile(`^([A-Z_]+)="([^"\\$` + "`" + `]*)"$`)
)

// launchConfig is F16.
func (f Front) launchConfig() term.ExitCode {
	if len(f.Args) != 2 {
		var p term.Printer
		p.Print(f.Stderr, launchConfigUsage)
		if p.Err() != nil {
			return term.ExitFailure
		}
		return term.ExitUsage
	}
	path := f.Args[1]
	out, err := readLaunchConfig(path)
	if err != nil {
		return f.fail(fmt.Sprintf("ccy: cannot read the saved launch configuration %s: %v", path, err))
	}
	var p term.Printer
	p.Print(f.Stdout, out)
	if err := p.Err(); err != nil {
		return f.fail("ccy: could not write the saved launch configuration: " + err.Error())
	}
	return term.ExitOK
}

// readLaunchConfig is the file at path as `KEY<TAB>value` lines, or why it does not read.
func readLaunchConfig(path string) (string, error) {
	text, err := os.ReadFile(path)
	if err != nil {
		if pathErr, ok := errors.AsType[*os.PathError](err); ok {
			return "", pathErr.Err
		}
		return "", err
	}
	if len(text) == 0 {
		return "", nil
	}
	if text[len(text)-1] != '\n' {
		return "", errors.New("it does not end in a newline (cut short?)")
	}
	var out strings.Builder
	var seen []string
	for i, line := range bytes.Split(text[:len(text)-1], []byte("\n")) {
		key, value, err := launchConfigLine(string(line))
		if err != nil {
			return "", fmt.Errorf("line %d: %w", i+1, err)
		}
		if key == "" {
			continue
		}
		if slices.Contains(seen, key) {
			return "", fmt.Errorf("line %d: %s is set twice", i+1, key)
		}
		seen = append(seen, key)
		out.WriteString(key + "\t" + value + "\n")
	}
	return out.String(), nil
}

// launchConfigLine is one line's key and value; no key for a comment.
func launchConfigLine(line string) (string, string, error) {
	if !utf8.ValidString(line) {
		return "", "", errors.New("it is not UTF-8 text")
	}
	if strings.ContainsFunc(line, unicode.IsControl) {
		return "", "", errors.New("it holds a control character")
	}
	if strings.HasPrefix(line, "#") {
		return "", "", nil
	}
	if m := formatLine.FindStringSubmatch(line); m != nil {
		return formatKey, m[1], nil
	}
	m := quotedLine.FindStringSubmatch(line)
	if m == nil || m[1] == formatKey {
		return "", "", fmt.Errorf("it is not KEY=\"value\" as ccy writes it: %q", line)
	}
	if !slices.Contains(launchConfigKeys, m[1]) {
		return "", "", fmt.Errorf("%s is not a key ccy writes", m[1])
	}
	return m[1], m[2], nil
}
