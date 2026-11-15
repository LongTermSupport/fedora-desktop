package matrixclients

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"
)

const twoClients = `{
  "clients": [
    {"name": "Element", "app_id": "im.riot.Riot", "keyring": true,
     "native": [{"parent": ".config", "name": "Element", "profiles": true}]},
    {"name": "Nheko", "app_id": "im.nheko.Nheko", "keyring": true, "native": []}
  ],
  "legacy_consent": ["im.riot.Riot"]
}`

func twoClientList(t *testing.T) List {
	t.Helper()
	list, err := Parse([]byte(twoClients))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return list
}

// The embedded list loads, names at least Element, and every client's paths are derived from it.
func TestTheEmbeddedListLoads(t *testing.T) {
	list, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(list.Clients, func(c Client) bool { return c.AppID == "im.riot.Riot" }) {
		t.Fatalf("Element is not listed: %+v", list.Clients)
	}
}

func TestPathsAreDerivedFromTheList(t *testing.T) {
	list := twoClientList(t)
	if got := list.ProfilePaths(); !slices.Equal(got, []string{".var/app/im.riot.Riot", ".config/Element", ".var/app/im.nheko.Nheko"}) {
		t.Fatalf("paths %v", got)
	}
	if got := list.ProfilePrefixes(); !slices.Equal(got, []string{".config/Element-"}) {
		t.Fatalf("prefixes %v", got)
	}
	if got := list.Names([]string{"im.nheko.Nheko", "im.riot.Riot"}); got != "Element and Nheko" {
		t.Fatalf("names %q", got)
	}
}

// A list the readers could disagree on is refused.
func TestABadListIsRefused(t *testing.T) {
	bad := map[string]string{
		"unknown key":       `{"clients": [], "legacy_consent": [], "extra": 1}`,
		"no clients":        `{"clients": [], "legacy_consent": []}`,
		"bad app id":        strings.Replace(twoClients, "im.nheko.Nheko", "nheko", 1),
		"app id twice":      strings.Replace(twoClients, "im.nheko.Nheko", "im.riot.Riot", 1),
		"empty name":        strings.Replace(twoClients, `"Nheko"`, `""`, 1),
		"parent escapes":    strings.Replace(twoClients, `".config"`, `"../x"`, 1),
		"name with a slash": strings.Replace(twoClients, `"name": "Element", "profiles"`, `"name": "a/b", "profiles"`, 1),
		"legacy unlisted":   strings.Replace(twoClients, `"legacy_consent": ["im.riot.Riot"]`, `"legacy_consent": ["org.x.Y"]`, 1),
		"trailing data":     twoClients + "{}",
	}
	for name, text := range bad {
		if _, err := Parse([]byte(text)); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

// The consent record: a legacy record (no clients line) accepts the legacy clients; a record
// with one clients line accepts what it names; anything else is not a record.
func TestTheConsentRecordIsRead(t *testing.T) {
	list := twoClientList(t)
	good := map[string][]string{
		"":                                {"im.riot.Riot"},
		"accepted\n":                      {"im.riot.Riot"},
		"accepted 2026-10-15T12:30:00Z\n": {"im.riot.Riot"},
		"accepted x\nclients\n":           {},
		"clients im.nheko.Nheko\n":        {"im.nheko.Nheko"},
		"accepted x\nclients im.riot.Riot im.nheko.Nheko\n": {"im.riot.Riot", "im.nheko.Nheko"},
		"clients org.other.Client":                          {"org.other.Client"},
	}
	for text, want := range good {
		got, err := list.ParseConsent([]byte(text))
		if err != nil || !slices.Equal(got, want) {
			t.Errorf("%q: %v %v, want %v", text, got, err, want)
		}
	}
	bad := []string{
		"clients im.riot.Riot\nclients im.nheko.Nheko\n",
		"clients  im.riot.Riot\n",
		"clients im.riot.Riot \n",
		"clients im.riot.Riot im.riot.Riot\n",
		"clients riot\n",
		"clients\tim.riot.Riot\n",
		strings.Repeat("x", ConsentMaxBytes+1),
	}
	for _, text := range bad {
		if got, err := list.ParseConsent([]byte(text)); err == nil {
			t.Errorf("%q: accepted as %v", text, got)
		}
	}
}

func TestTheConsentRecordIsWrittenAsItIsRead(t *testing.T) {
	list := twoClientList(t)
	at := time.Date(2026, 10, 15, 12, 30, 0, 0, time.UTC)
	body := FormatConsent(at, []string{"im.riot.Riot", "im.nheko.Nheko"})
	if string(body) != "accepted 2026-10-15T12:30:00Z\nclients im.riot.Riot im.nheko.Nheko\n" {
		t.Fatalf("record %q", body)
	}
	if got, err := list.ParseConsent(body); err != nil || !slices.Equal(got, []string{"im.riot.Riot", "im.nheko.Nheko"}) {
		t.Fatalf("read back %v %v", got, err)
	}
	if string(FormatConsent(at, nil)) != "accepted 2026-10-15T12:30:00Z\nclients\n" {
		t.Fatalf("empty record %q", FormatConsent(at, nil))
	}
}

// makeHome is a home holding an Element profile and a dangling link where Nheko's Flatpak data
// would be.
func makeHome(t *testing.T) string {
	t.Helper()
	home := t.TempDir()
	for _, dir := range []string{".config/Element-work", ".var/app"} {
		if err := os.MkdirAll(filepath.Join(home, dir), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Symlink("gone", filepath.Join(home, ".var/app/im.nheko.Nheko")); err != nil {
		t.Fatal(err)
	}
	return home
}

// Present finds each client's Flatpak tree (a dangling link counts) and its native profiles, in
// list order; an empty home has none.
func TestPresentFindsEveryClientsProfile(t *testing.T) {
	list := twoClientList(t)
	if found, err := list.Present(t.TempDir()); err != nil || len(found) != 0 {
		t.Fatalf("empty home: %v %v", found, err)
	}
	home := makeHome(t)
	found, err := list.Present(home)
	if err != nil || len(found) != 2 || found[0].Client.Name != "Element" || found[1].Client.Name != "Nheko" ||
		found[0].Path != filepath.Join(home, ".config/Element-work") {
		t.Fatalf("found %+v %v", found, err)
	}
	if got := Uncovered(found, []string{"im.riot.Riot"}); len(got) != 1 || got[0].AppID != "im.nheko.Nheko" {
		t.Fatalf("uncovered %+v", got)
	}
	if got := Uncovered(found, []string{"im.riot.Riot", "im.nheko.Nheko"}); len(got) != 0 {
		t.Fatalf("all covered, got %+v", got)
	}
}

// Plan 00254 Task 5.4: the list's Lightning entry names the folder each team's own instance lives
// in (teams/<team>/{data,config,cache} under its Flatpak data), so a session there is found as
// one under the app's default root is.
func TestLightningsTeamInstancesAreListed(t *testing.T) {
	list, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	at := slices.IndexFunc(list.Clients, func(c Client) bool { return c.Name == "Lightning" })
	if at < 0 {
		t.Fatal("Lightning is not listed")
	}
	lightning := list.Clients[at]
	teams := lightning.FlatpakDir() + "/teams"
	if !slices.Contains(list.ProfilePaths(), teams) {
		t.Fatalf("the profile paths %v do not name %s", list.ProfilePaths(), teams)
	}
	home := t.TempDir()
	mustDo(t, os.MkdirAll(filepath.Join(home, teams, "team-a", "data"), 0o700))
	found, err := list.Present(home)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(found, func(f Found) bool { return f.Client.AppID == lightning.AppID && f.Path == filepath.Join(home, teams) }) {
		t.Fatalf("found %+v", found)
	}
}

// writeRecord is a consent record of this user's at home/.config/pingbus, mode 0600.
func writeRecord(t *testing.T, home, text string) string {
	t.Helper()
	dir := filepath.Join(home, AcceptedDir)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	record := filepath.Join(dir, AcceptedFile)
	if err := os.WriteFile(record, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	return record
}

// Accepted is config.accepted_clients: the record's app ids when it is a regular file of uid's
// that no one else can read or write, in a real directory of uid's, and reads as a record; nil
// otherwise. Another user's is said through the uid given, so no file is given away.
func TestAcceptedIsReadAsPingbusReadsIt(t *testing.T) {
	list := twoClientList(t)
	uid := os.Getuid()
	home := t.TempDir()
	if got := list.Accepted(home, uid); got != nil {
		t.Fatalf("no record: %v", got)
	}
	record := writeRecord(t, home, "accepted x\n")
	if got := list.Accepted(home, uid); !slices.Equal(got, []string{"im.riot.Riot"}) {
		t.Fatalf("a legacy record: %v", got)
	}
	if got := list.Accepted(home, uid+1); got != nil {
		t.Fatalf("another user's record: %v", got)
	}
	if err := os.WriteFile(record, []byte("accepted x\nclients\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := list.Accepted(home, uid); got == nil || len(got) != 0 {
		t.Fatalf("a record accepting nothing is a record: %#v", got)
	}
	for name, spoil := range map[string]func(){
		"readable by others": func() { widen(t, record) },
		"not a record":       func() { mustDo(t, os.WriteFile(record, []byte("clients riot\n"), 0o600)) },
		"a link":             func() { mustDo(t, os.Remove(record)); mustDo(t, os.Symlink(filepath.Join(home, "x"), record)) },
		"a directory":        func() { mustDo(t, os.Remove(record)); mustDo(t, os.Mkdir(record, 0o700)) },
	} {
		t.Run(name, func(t *testing.T) {
			home := t.TempDir()
			record = writeRecord(t, home, "accepted x\n")
			spoil()
			if got := list.Accepted(home, uid); got != nil {
				t.Fatalf("counted: %v", got)
			}
		})
	}
	linked := t.TempDir()
	writeRecord(t, filepath.Join(linked, "elsewhere"), "accepted x\n")
	mustDo(t, os.MkdirAll(filepath.Join(linked, ".config"), 0o700))
	mustDo(t, os.Symlink(filepath.Join(linked, "elsewhere", AcceptedDir), filepath.Join(linked, AcceptedDir)))
	if got := list.Accepted(linked, uid); got != nil {
		t.Fatalf("a linked directory: %v", got)
	}
}

func mustDo(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}

// widen gives a file's group and others read access, as a careless copy would.
func widen(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	mustDo(t, err)
	mustDo(t, os.Chmod(path, info.Mode().Perm()|0o044))
}
