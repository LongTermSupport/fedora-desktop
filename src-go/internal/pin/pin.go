// Package pin reads tuwunel.pin: the release version and the sha256 of each architecture's
// asset. The release is unsigned, so the pinned hash is the only supply-chain check; the
// version and hashes become URL and file names, so each value is held to a strict shape.
// Every refusal wraps ErrInvalid, the configuration class (exit 78).
package pin

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ErrInvalid is wrapped by every refusal: a missing or unreadable pin file, a line that is
// not "key: value", an unknown key, a malformed or missing value, an unpinned architecture.
var ErrInvalid = errors.New("invalid pin")

const (
	keyVersion = "tuwunel_version"
	keyX8664   = "tuwunel_sha256_x86_64"
	keyAarch64 = "tuwunel_sha256_aarch64"

	releaseURL = "https://github.com/matrix-construct/tuwunel/releases/download/"
)

var (
	linePattern    = regexp.MustCompile(`^([a-z0-9_]+): *([^ ]+) *$`)
	versionPattern = regexp.MustCompile(`^v[0-9]+\.[0-9]+\.[0-9]+$`)
	hashPattern    = regexp.MustCompile(`^[0-9a-f]{64}$`)
)

// Pin is a parsed tuwunel.pin.
type Pin struct {
	Version       string // "v1.2.3"
	SHA256X8664   string
	SHA256Aarch64 string
}

// Build is what one architecture downloads and installs.
type Build struct {
	Arch   string // the asset's architecture tag, e.g. "x86_64-v1"
	SHA256 string
	Asset  string // the file name of the release asset
	URL    string
	Binary string // the installed file name, e.g. "tuwunel-1.2.3"
}

// Bare is the version without its leading "v".
func (p Pin) Bare() string {
	return strings.TrimPrefix(p.Version, "v")
}

// ForMachine is the build for a `uname -m` value, or ErrInvalid for an unpinned one.
func (p Pin) ForMachine(machine string) (Build, error) {
	var arch, sum string
	switch machine {
	case "x86_64":
		arch, sum = "x86_64-v1", p.SHA256X8664
	case "aarch64":
		arch, sum = "aarch64-v8", p.SHA256Aarch64
	default:
		return Build{}, refusal("no pinned Tuwunel build for architecture " + machine)
	}
	asset := p.Version + "-release-all-" + arch + "-linux-gnu-tuwunel.zst"
	return Build{
		Arch:   arch,
		SHA256: sum,
		Asset:  asset,
		URL:    releaseURL + p.Version + "/" + asset,
		Binary: "tuwunel-" + p.Bare(),
	}, nil
}

// Load reads and parses the pin file at path.
func Load(path string) (Pin, error) {
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return Pin{}, refusal(path + " is missing")
	}
	return Parse(path, data)
}

// Parse reads a pin file. name is only used in messages. A blank line and a line starting
// with "#" are skipped; a later duplicate of a key replaces the earlier one.
func Parse(name string, data []byte) (Pin, error) {
	values := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		m := linePattern.FindStringSubmatch(line)
		if m == nil {
			return Pin{}, refuse(name, "unreadable line '%s'", line)
		}
		key, value := m[1], m[2]
		var ok bool
		switch key {
		case keyVersion:
			ok = versionPattern.MatchString(value)
		case keyX8664, keyAarch64:
			ok = hashPattern.MatchString(value)
		default:
			return Pin{}, refuse(name, "unknown key %s", key)
		}
		if !ok {
			return Pin{}, refuse(name, "%s has a malformed value", key)
		}
		values[key] = value
	}
	for _, key := range []string{keyVersion, keyX8664, keyAarch64} {
		if values[key] == "" {
			return Pin{}, refuse(name, "%s is missing", key)
		}
	}
	return Pin{Version: values[keyVersion], SHA256X8664: values[keyX8664], SHA256Aarch64: values[keyAarch64]}, nil
}

// refusal is an ErrInvalid whose text is exactly the message the user sees.
type refusal string

func (r refusal) Error() string { return string(r) }

func (r refusal) Is(target error) bool { return target == ErrInvalid }

func refuse(name, format string, args ...any) error {
	return refusal(name + ": " + fmt.Sprintf(format, args...))
}
