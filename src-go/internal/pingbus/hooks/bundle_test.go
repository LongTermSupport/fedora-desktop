package hooks

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testRoom = "!RRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRRR"

func memberJSON(team, handle string) string {
	return `{"protocol": 1, "team": "` + team + `", "user_id": "@` + handle + `:hs.example.org", "server_name": "hs.example.org",` +
		` "base_url": "http://127.0.0.1:1", "plain_http_hosts": ["127.0.0.1"], "token_file": "token",` +
		` "admin": "@admin:hs.example.org", "room": "` + testRoom + `"}`
}

// makeBundle writes a plain bundle for team under home: member.json and a 0600 token.
func makeBundle(t *testing.T, home, team, handle, token string) string {
	t.Helper()
	dir := filepath.Join(home, team)
	writeFile(t, filepath.Join(dir, memberFile), memberJSON(team, handle), 0o600)
	writeFile(t, filepath.Join(dir, tokenFile), token, 0o600)
	return dir
}

func noUserHome(int) (string, error) { return "", errors.New("no passwd entry") }

func testWorld() *world { return &world{uid: os.Getuid(), userHome: noUserHome} }

// H16 resolve_home.
func TestResolveHome(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want string
	}{
		{map[string]string{"PINGBUS_HOME": "/p/", "HOME": "/h"}, "/p"},
		{map[string]string{"XDG_CONFIG_HOME": "/x", "HOME": "/h"}, "/x/pingbus"},
		{map[string]string{"XDG_CONFIG_HOME": "rel", "HOME": "/h"}, "/h/.config/pingbus"},
		{map[string]string{"PINGBUS_HOME": "", "HOME": "/h"}, "/h/.config/pingbus"},
	}
	for _, c := range cases {
		if got, err := resolveHome(c.env); err != nil || got != c.want {
			t.Errorf("resolveHome(%v) = %q, %v; want %q", c.env, got, err, c.want)
		}
	}
	for _, env := range []map[string]string{{"PINGBUS_HOME": "rel"}, {"HOME": "rel"}, {}} {
		if _, err := resolveHome(env); !errors.As(err, new(*configError)) {
			t.Errorf("resolveHome(%v): %v, want a config error", env, err)
		}
	}
}

// H17 active_teams: PINGBUS_TEAMS without the membership; the membership's teams with it.
func TestActiveTeams(t *testing.T) {
	home := t.TempDir()
	if got, err := activeTeams(map[string]string{"PINGBUS_TEAMS": "alpha,beta"}, home); err != nil || strings.Join(got, ",") != "alpha,beta" {
		t.Errorf("PINGBUS_TEAMS: %v %v", got, err)
	}
	for _, teams := range []string{"", "alpha,,beta", "alpha,alpha", "Alpha"} {
		if _, err := activeTeams(map[string]string{"PINGBUS_TEAMS": teams}, home); !errors.As(err, new(*configError)) {
			t.Errorf("PINGBUS_TEAMS=%q: %v, want a config error", teams, err)
		}
	}
	mount := t.TempDir()
	env := map[string]string{membershipVar: mount, "PINGBUS_TEAMS": "ignored"}
	writeFile(t, filepath.Join(mount, desiredFile), desiredJSON(`{"team": "beta", "seat": "2"}, {"team": "alpha", "seat": "1"}`, ""), 0o600)
	if _, err := activeTeams(env, home); !errors.As(err, new(*configError)) {
		t.Errorf("no applied file: %v, want config (no team is active)", err)
	}
	writeFile(t, filepath.Join(home, appliedFile), appliedJSON(heldSeat("alpha", "1", handleA)+", "+heldSeat("beta", "2", handleB)), 0o600)
	if got, err := activeTeams(env, home); err != nil || strings.Join(got, ",") != "beta,alpha" {
		t.Errorf("membership: %v %v (desired order)", got, err)
	}
	for _, marker := range []string{"off", "relative"} {
		if _, err := activeTeams(map[string]string{membershipVar: marker}, home); !errors.As(err, new(*configError)) {
			t.Errorf("%s=%q: %v, want config", membershipVar, marker, err)
		}
	}
}

// H18 load_bundle and check_distinct over real bundles.
func TestLoadActive(t *testing.T) {
	home := t.TempDir()
	makeBundle(t, home, "alpha", handleA, "tok-a")
	makeBundle(t, home, "beta", handleB, "tok-b")
	w := testWorld()
	env := map[string]string{"PINGBUS_HOME": home, "PINGBUS_TEAMS": "alpha,beta"}
	members, err := w.loadActive(env)
	if err != nil || len(members) != 2 || members[1].team != "beta" || members[0].bundleDir != filepath.Join(home, "alpha") {
		t.Fatalf("members: %+v %v", members, err)
	}
	if token, err := w.readToken(members[0]); err != nil || token != "tok-a" {
		t.Errorf("token: %q %v", token, err)
	}
	makeBundle(t, home, "gamma", "proj.3+box.podman", "tok-a")
	if _, err := w.loadActive(map[string]string{"PINGBUS_HOME": home, "PINGBUS_TEAMS": "alpha,gamma"}); err == nil || !strings.Contains(err.Error(), "share one token") {
		t.Errorf("a shared token: %v", err)
	}
	makeBundle(t, home, "delta", handleA, "tok-d")
	if _, err := w.loadActive(map[string]string{"PINGBUS_HOME": home, "PINGBUS_TEAMS": "alpha,delta"}); err == nil || !strings.Contains(err.Error(), "share one user_id") {
		t.Errorf("a shared user: %v", err)
	}
	if _, err := w.loadActive(map[string]string{"PINGBUS_HOME": home, "PINGBUS_TEAMS": "absent"}); !errors.As(err, new(*configError)) {
		t.Errorf("no bundle: %v", err)
	}
}

func TestLoadBundleRefusals(t *testing.T) {
	w := testWorld()
	cases := map[string]func(t *testing.T, dir string){
		"loose token": func(t *testing.T, dir string) { mustChmod(t, filepath.Join(dir, tokenFile), 0o640) },
		"token newline": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, tokenFile), "tok\n", 0o600)
		},
		"token long": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, tokenFile), strings.Repeat("t", tokenMaxBytes+1), 0o600)
		},
		"token link": func(t *testing.T, dir string) {
			mustRemove(t, filepath.Join(dir, tokenFile))
			mustSymlink(t, filepath.Join(dir, memberFile), filepath.Join(dir, tokenFile))
		},
		"no token":         func(t *testing.T, dir string) { mustRemove(t, filepath.Join(dir, tokenFile)) },
		"member not JSON":  func(t *testing.T, dir string) { writeFile(t, filepath.Join(dir, memberFile), "{", 0o600) },
		"member duplicate": func(t *testing.T, dir string) { writeFile(t, filepath.Join(dir, memberFile), `{"a": 1, "a": 2}`, 0o600) },
		"member array":     func(t *testing.T, dir string) { writeFile(t, filepath.Join(dir, memberFile), `[]`, 0o600) },
		"member fields": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, memberFile), strings.Replace(memberJSON("alpha", handleA), `"protocol": 1`, `"protocol": 2`, 1), 0o600)
		},
		"member large": func(t *testing.T, dir string) {
			writeFile(t, filepath.Join(dir, memberFile), memberJSON("alpha", handleA)+strings.Repeat(" ", memberMaxBytes), 0o600)
		},
		"member dir": func(t *testing.T, dir string) {
			mustRemove(t, filepath.Join(dir, memberFile))
			if err := os.Mkdir(filepath.Join(dir, memberFile), 0o700); err != nil {
				t.Fatal(err)
			}
		},
	}
	for name, spoil := range cases {
		home := t.TempDir()
		dir := makeBundle(t, home, "alpha", handleA, "tok")
		spoil(t, dir)
		_, err := w.loadBundle(home, "alpha")
		if err == nil {
			_, err = w.readToken(member{team: "alpha", bundleDir: dir})
		}
		if !errors.As(err, new(*configError)) {
			t.Errorf("%s: %v, want a config error", name, err)
		}
	}
	if _, err := w.loadBundle(t.TempDir(), "Bad"); !errors.As(err, new(*configError)) {
		t.Errorf("a bad team name: %v", err)
	}
}

// _check_linked_bundle: a seat link must reach a real 0700 directory of this user's.
func TestLinkedBundle(t *testing.T) {
	w := testWorld()
	home, seats := t.TempDir(), t.TempDir()
	seat := makeBundle(t, filepath.Join(seats, "alpha"), "1", handleA, "tok")
	mustRewriteMember(t, seat, "alpha")
	mustChmod(t, seat, 0o700)
	mustSymlink(t, seat, filepath.Join(home, "alpha"))
	if m, err := w.loadBundle(home, "alpha"); err != nil || m.bundleDir != filepath.Join(home, "alpha") {
		t.Fatalf("linked: %+v %v", m, err)
	}
	mustChmod(t, seat, 0o755)
	if _, err := w.loadBundle(home, "alpha"); err == nil || !strings.Contains(err.Error(), "mode 0755") {
		t.Errorf("a loose seat: %v", err)
	}
	mustRemove(t, filepath.Join(home, "alpha"))
	mustSymlink(t, filepath.Join(seats, "missing"), filepath.Join(home, "alpha"))
	if _, err := w.loadBundle(home, "alpha"); err == nil || !strings.Contains(err.Error(), "no such directory") {
		t.Errorf("a dangling link: %v", err)
	}
	mustRemove(t, filepath.Join(home, "alpha"))
	mustSymlink(t, filepath.Join(seat, memberFile), filepath.Join(home, "alpha"))
	if _, err := w.loadBundle(home, "alpha"); err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("a link to a file: %v", err)
	}
	mustRemove(t, filepath.Join(home, "alpha"))
	mustSymlink(t, filepath.Join(home, "other"), filepath.Join(home, "alpha"))
	mustSymlink(t, seat, filepath.Join(home, "other"))
	if _, err := w.loadBundle(home, "alpha"); err == nil || !strings.Contains(err.Error(), "link onto a link") {
		t.Errorf("a link onto a link: %v", err)
	}
}

// H18, HB6: a host member, looked for beside an Element profile under the passwd home; no
// passwd entry is config, not internal.
func TestHostMemberBesideElement(t *testing.T) {
	home := t.TempDir()
	makeBundle(t, home, "alpha", "proj.desk+box.host", "tok")
	user := t.TempDir()
	w := &world{uid: os.Getuid(), userHome: func(int) (string, error) { return user, nil }}
	if _, err := w.loadBundle(home, "alpha"); err != nil {
		t.Errorf("no Element: %v", err)
	}
	if err := os.MkdirAll(filepath.Join(user, ".config", "Element-work"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := w.loadBundle(home, "alpha"); err == nil || !strings.Contains(err.Error(), "Element profile") {
		t.Errorf("beside Element: %v", err)
	}
	if _, err := testWorld().loadBundle(home, "alpha"); !errors.As(err, new(*configError)) {
		t.Errorf("no passwd entry: %v, want config (HB6)", err)
	}
	flatpak := t.TempDir()
	if err := os.MkdirAll(filepath.Join(flatpak, ".var", "app"), 0o700); err != nil {
		t.Fatal(err)
	}
	mustSymlink(t, "/nonexistent", filepath.Join(flatpak, ".var", "app", "im.riot.Riot"))
	w.userHome = func(int) (string, error) { return flatpak, nil }
	if _, err := w.loadBundle(home, "alpha"); err == nil {
		t.Error("a dangling Flatpak link counts as Element")
	}
	// Reached through a seat link, the owner's acceptance lets it run beside Element.
	seats := t.TempDir()
	seat := filepath.Join(seats, "alpha", "desk")
	if err := os.MkdirAll(filepath.Dir(seat), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(home, "alpha"), seat); err != nil {
		t.Fatal(err)
	}
	mustChmod(t, seat, 0o700)
	mustSymlink(t, seat, filepath.Join(home, "alpha"))
	writeFile(t, filepath.Join(flatpak, ".config", "pingbus", "element-accepted"), "", 0o600)
	if _, err := w.loadBundle(home, "alpha"); err != nil {
		t.Errorf("accepted beside Element: %v", err)
	}
}

func mustChmod(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}

func mustRemove(t *testing.T, path string) {
	t.Helper()
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
}

func mustSymlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

// mustRewriteMember makes the seat directory's member.json name team (a seat is named by its
// seat, its bundle by its team).
func mustRewriteMember(t *testing.T, dir, team string) {
	t.Helper()
	writeFile(t, filepath.Join(dir, memberFile), memberJSON(team, handleA), 0o600)
}
