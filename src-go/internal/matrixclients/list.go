// Package matrixclients is the one list of the Matrix desktop clients this repository installs,
// read by every Go guard that keeps an agent away from a human's Matrix session (issue #251).
//
// The list is helpers/pingbus/matrix_clients.json, which pingbus (Python) reads at run time;
// clients.json here is its copy, embedded, and this package's tests hold the two equal byte for
// byte. Each client is a Flatpak whose data lives under ~/.var/app/<app_id>, with any native
// install's folders (parent/name, and parent/name-<profile> when it has profiles), and whether it
// keeps its session in the keyring. The same file names, in legacy_consent, the clients a consent
// record written before the record named its clients was given for.
//
// The consent record (~/.config/pingbus/element-accepted) is read here as pingbus reads it
// (config.py parse_consent): one line "clients[ <app id>...]" names the clients the owner
// accepted a host seat beside; a record without that line is an older one, given for the
// legacy_consent clients alone.
package matrixclients

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"
)

//go:embed clients.json
var listJSON []byte

// ConsentMaxBytes is the most a consent record may hold.
const ConsentMaxBytes = 4096

// flatpakData is where a Flatpak keeps its data, under a home.
const flatpakData = ".var/app/"

// appIDPattern is a Flatpak app id: three or more dotted segments.
var appIDPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_-]*){2,}$`)

// Native is a native install's folder under a home: Parent/Name, and Parent/Name-<profile>.
type Native struct {
	Parent   string `json:"parent"`
	Name     string `json:"name"`
	Profiles bool   `json:"profiles"`
}

// Client is one Matrix desktop client.
type Client struct {
	Name    string   `json:"name"`
	AppID   string   `json:"app_id"`
	Keyring bool     `json:"keyring"`
	Native  []Native `json:"native"`
}

// FlatpakDir is the client's Flatpak data folder, relative to a home.
func (c Client) FlatpakDir() string { return flatpakData + c.AppID }

// List is the clients, and those an older consent record was given for.
type List struct {
	Clients       []Client `json:"clients"`
	LegacyConsent []string `json:"legacy_consent"`
}

// Load is the embedded list.
func Load() (List, error) { return Parse(listJSON) }

// Parse reads a list, refusing unknown keys, trailing data and any entry the readers could take
// differently.
func Parse(data []byte) (List, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var list List
	if err := dec.Decode(&list); err != nil {
		return List{}, fmt.Errorf("the Matrix client list: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return List{}, errors.New("the Matrix client list: data after the object")
	}
	return list, list.validate()
}

// ProfilePaths are the folders, relative to a home, whose presence is a client's profile: each
// Flatpak's data, and each native Parent/Name, in list order.
func (l List) ProfilePaths() []string {
	var paths []string
	for _, c := range l.Clients {
		paths = append(paths, c.FlatpakDir())
		for _, n := range c.Native {
			paths = append(paths, n.Parent+"/"+n.Name)
		}
	}
	return paths
}

// ProfilePrefixes are the native Parent/Name- whose entries are named profiles.
func (l List) ProfilePrefixes() []string {
	var prefixes []string
	for _, c := range l.Clients {
		for _, n := range c.Native {
			if n.Profiles {
				prefixes = append(prefixes, n.Parent+"/"+n.Name+"-")
			}
		}
	}
	return prefixes
}

// Names is the listed clients among ids in words, in list order: "Element", "Element and
// Nheko", "Element, Nheko and Lightning". An id not on the list is left out.
func (l List) Names(ids []string) string {
	var names []string
	for _, c := range l.Clients {
		if slices.Contains(ids, c.AppID) {
			names = append(names, c.Name)
		}
	}
	return Words(names)
}

// Words joins names as a sentence does.
func Words(names []string) string {
	if len(names) < 2 {
		return strings.Join(names, "")
	}
	return strings.Join(names[:len(names)-1], ", ") + " and " + names[len(names)-1]
}

// AppIDs are the clients' app ids, in order.
func AppIDs(clients []Client) []string {
	ids := make([]string, 0, len(clients))
	for _, c := range clients {
		ids = append(ids, c.AppID)
	}
	return ids
}

// Found is a client's profile found under a home.
type Found struct {
	Client Client
	Path   string
}

// Present is every listed client's profile under home, in list order: its Flatpak data folder
// (a dangling link counts: something put it there), then each native folder and, for a client
// with profiles, each Name-<profile> beside it. A parent folder that cannot be listed is an
// error, since it could hide a profile.
func (l List) Present(home string) ([]Found, error) {
	var found []Found
	for _, c := range l.Clients {
		flatpak := filepath.Join(home, c.FlatpakDir())
		if _, err := os.Lstat(flatpak); err == nil {
			found = append(found, Found{c, flatpak})
		}
		for _, n := range c.Native {
			paths, err := nativeProfiles(home, c, n)
			if err != nil {
				return nil, err
			}
			for _, p := range paths {
				found = append(found, Found{c, p})
			}
		}
	}
	return found, nil
}

// Matches is whether an entry of a native folder's parent is that folder or one of its profiles.
func (n Native) Matches(entry string) bool {
	return entry == n.Name || (n.Profiles && strings.HasPrefix(entry, n.Name+"-"))
}

// Uncovered are the clients found whose app id accepted does not name, each once, in order.
func Uncovered(found []Found, accepted []string) []Client {
	var out []Client
	for _, f := range found {
		if !slices.Contains(accepted, f.Client.AppID) && !slices.ContainsFunc(out, func(c Client) bool { return c.AppID == f.Client.AppID }) {
			out = append(out, f.Client)
		}
	}
	return out
}

// ParseConsent is the app ids a consent record accepts: those its one clients line names, or,
// for a record without one, the legacy_consent clients. Anything else is not a record.
func (l List) ParseConsent(data []byte) ([]string, error) {
	if len(data) > ConsentMaxBytes {
		return nil, fmt.Errorf("the consent record is larger than %d bytes", ConsentMaxBytes)
	}
	var line []byte
	found := 0
	for raw := range bytes.SplitSeq(data, []byte("\n")) {
		if bytes.HasPrefix(raw, []byte("clients")) {
			line = raw
			found++
		}
	}
	switch {
	case found == 0:
		return slices.Clone(l.LegacyConsent), nil
	case found > 1:
		return nil, errors.New("the consent record has more than one clients line")
	case !bytes.Equal(line, []byte("clients")) && !bytes.HasPrefix(line, []byte("clients ")):
		return nil, errors.New("the consent record's clients line is not \"clients\" and its app ids, one space apart")
	}
	ids := []string{}
	for _, word := range bytes.Split(line, []byte(" "))[1:] {
		id := string(word)
		if !appIDPattern.MatchString(id) {
			return nil, fmt.Errorf("the consent record's clients line holds %q, which is not an app id", id)
		}
		if slices.Contains(ids, id) {
			return nil, fmt.Errorf("the consent record names %s twice", id)
		}
		ids = append(ids, id)
	}
	return ids, nil
}

// FormatConsent is the record of a consent given at the time when, for ids.
func FormatConsent(when time.Time, ids []string) []byte {
	line := strings.Join(append([]string{"clients"}, ids...), " ")
	return []byte("accepted " + when.UTC().Format("2006-01-02T15:04:05Z") + "\n" + line + "\n")
}

func (l List) validate() error {
	if len(l.Clients) == 0 {
		return errors.New("the Matrix client list names no client")
	}
	seen := map[string]bool{}
	for i, c := range l.Clients {
		if err := c.validate(i, seen); err != nil {
			return err
		}
		seen[c.AppID] = true
	}
	for _, id := range l.LegacyConsent {
		if !seen[id] {
			return fmt.Errorf("the Matrix client list: legacy_consent names %s, which is not listed", id)
		}
	}
	return nil
}

func (c Client) validate(i int, seen map[string]bool) error {
	switch {
	case c.Name == "":
		return fmt.Errorf("the Matrix client list: client %d has no name", i+1)
	case !appIDPattern.MatchString(c.AppID):
		return fmt.Errorf("the Matrix client list: %s: %q is not a Flatpak app id", c.Name, c.AppID)
	case seen[c.AppID]:
		return fmt.Errorf("the Matrix client list: %s is listed twice", c.AppID)
	}
	for _, n := range c.Native {
		if !relativeDir(n.Parent) || n.Name == "" || strings.ContainsAny(n.Name, "/*") || n.Name == "." || n.Name == ".." {
			return fmt.Errorf("the Matrix client list: %s: a native folder must be a relative parent and a plain name", c.Name)
		}
	}
	return nil
}

// relativeDir is a home-relative folder with no empty, "." or ".." part.
func relativeDir(dir string) bool {
	if dir == "" || strings.HasPrefix(dir, "/") || strings.Contains(dir, "*") {
		return false
	}
	for part := range strings.SplitSeq(dir, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

// nativeProfiles are n's folders under home, from a listing of their parent; a parent that is
// not a folder holds none.
func nativeProfiles(home string, c Client, n Native) ([]string, error) {
	parent := filepath.Join(home, n.Parent)
	if !isDir(parent) {
		return nil, nil
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		return nil, fmt.Errorf("%s cannot be listed to look for a %s profile (%w)", parent, c.Name, err)
	}
	var paths []string
	for _, e := range entries {
		if n.Matches(e.Name()) {
			paths = append(paths, filepath.Join(parent, e.Name()))
		}
	}
	return paths, nil
}

// isDir is whether path is a folder, through a link (pathlib's is_dir).
func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
