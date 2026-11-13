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
