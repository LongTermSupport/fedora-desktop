package lightning

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/matrixclients"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

const (
	appID   = "org.lightning_matrix.Lightning"
	baseURL = "http://192.0.2.10:8448"
	euid    = 1000
)

// world is one run's home.
type world struct {
	t    *testing.T
	home string
}

// result is what one run did: its exit code and what it printed.
type result struct {
	code        term.ExitCode
	out, errOut string
}

func newWorld(t *testing.T) *world {
	t.Helper()
	return &world{t: t, home: t.TempDir()}
}

func (w *world) app(args []string, stdin string, stdout *bytes.Buffer, stderr *bytes.Buffer) App {
	return App{
		Args: args, Stdin: strings.NewReader(stdin), Stdout: stdout, Stderr: stderr,
		Euid: euid, UID: os.Getuid(),
		Home: func(int) (string, error) { return w.home, nil },
	}
}

func (w *world) run(args []string, stdin string) result {
	w.t.Helper()
	var out, errOut bytes.Buffer
	code := w.app(args, stdin, &out, &errOut).Run()
	return result{code, out.String(), errOut.String()}
}

func (w *world) apply(stdin string) result {
	w.t.Helper()
	return w.run([]string{"apply"}, stdin)
}

func (w *world) root() string { return filepath.Join(w.home, ".var/app", appID) }

func (w *world) team(team string) string { return filepath.Join(w.root(), "teams", team) }

func (w *world) launcher(team string) string {
	return filepath.Join(w.home, ".local/share/applications", "agent-bus-"+team+"-lightning.desktop")
}

// settings is team-a's settings file.
func (w *world) settings() string {
	return filepath.Join(w.team("team-a"), "config/MatrixClient/matrix-client.conf")
}

func (w *world) mode(path string) fs.FileMode {
	w.t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		w.t.Fatal(err)
	}
	return info.Mode().Perm()
}

func (w *world) read(path string) string {
	w.t.Helper()
	data, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		w.t.Fatal(err)
	}
	return string(data)
}

func (w *world) write(path, text string) {
	w.t.Helper()
	mkdir(w.t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func exists(path string) bool {
	info, err := os.Lstat(path)
	return err == nil && info != nil
}

func lightningTeam(team string) string {
	return `{"team": "` + team + `", "base_url": "` + baseURL + `", "client": "lightning"}`
}

func elementTeam(team string) string {
	return `{"team": "` + team + `", "base_url": "` + baseURL + `", "client": "element"}`
}

func list(entries ...string) string { return "[" + strings.Join(entries, ", ") + "]" }

// TestL1TheArgumentsAreOneVerb holds that the command is check or apply and nothing else.
func TestL1TheArgumentsAreOneVerb(t *testing.T) {
	w := newWorld(t)
	for _, args := range [][]string{nil, {"verify"}, {"apply", "check"}, {"--apply"}} {
		r := w.run(args, "[]")
		if r.code != term.ExitUsage || r.out != "" || !strings.Contains(r.errOut, "usage: agent-bus-lightning check|apply") {
			t.Errorf("%q: %+v", args, r)
		}
	}
}

// TestL2RootIsRefused holds that the command never runs as root, before reading anything.
func TestL2RootIsRefused(t *testing.T) {
	w := newWorld(t)
	var out, errOut bytes.Buffer
	app := w.app([]string{"apply"}, list(lightningTeam("team-a")), &out, &errOut)
	app.Euid = 0
	if code := app.Run(); code != term.ExitConfig || out.Len() != 0 || !strings.Contains(errOut.String(), "not root") {
		t.Fatalf("exit %d stdout %q stderr %q", code, out.String(), errOut.String())
	}
	if exists(w.root()) {
		t.Fatal("root wrote the Lightning tree")
	}
}

// TestL3AnInvalidListIsRefused holds each entry to element.py's rules, naming the team and key
// at fault and never the value.
func TestL3AnInvalidListIsRefused(t *testing.T) {
	https := "https://bus.example.com:8448"
	entry := func(extra string) string {
		return `[{"team": "team-a", "base_url": "` + baseURL + `"` + extra + `}]`
	}
	httpsEntry := func(caFile string) string {
		return `[{"team": "team-a", "base_url": "` + https + `", "ca_file": ` + caFile + `}]`
	}
	cases := map[string]struct{ stdin, want string }{
		"not JSON":              {"[", "not valid JSON"},
		"a duplicate key":       {`[{"team": "a", "team": "b"}]`, "not valid JSON"},
		"not a list":            {lightningTeam("team-a"), "must be a JSON array"},
		"an entry not object":   {`["team-a"]`, "entry 1: must be an object"},
		"an unknown key":        {entry(`, "port": 1`), "entry 1: the only keys are base_url, ca_file, client, server_name, team"},
		"no team":               {`[{"base_url": "` + baseURL + `"}]`, "entry 1: team is required"},
		"no base_url":           {`[{"team": "team-a"}]`, "entry 1: base_url is required"},
		"a bad team":            {`[{"team": "Team_A", "base_url": "` + baseURL + `"}]`, "entry 1: team must match"},
		"a team not a string":   {`[{"team": 1, "base_url": "` + baseURL + `"}]`, "entry 1: team must match"},
		"http to a name":        {`[{"team": "team-a", "base_url": "http://bus.example.com"}]`, "team team-a: base_url may use http://"},
		"a path":                {`[{"team": "team-a", "base_url": "` + baseURL + `/_matrix"}]`, "team team-a: base_url must have no path"},
		"a url not a string":    {`[{"team": "team-a", "base_url": 1}]`, "team team-a: base_url must be a URL of printable ASCII"},
		"bad brackets":          {`[{"team": "team-a", "base_url": "https://[bus"}]`, "team team-a: base_url is not a URL"},
		"server_name public":    {entry(`, "server_name": "a.example.com"`), "team team-a: server_name must end in .internal"},
		"server_name one label": {entry(`, "server_name": "internal"`), "team team-a: server_name must be a lower-case DNS name"},
		"server_name a number":  {entry(`, "server_name": 1`), "team team-a: server_name must be a DNS name"},
		"ca_file on http":       {entry(`, "ca_file": "/etc/a.pem"`), "team team-a: ca_file is only for"},
		"ca_file relative":      {httpsEntry(`"a.pem"`), "team team-a: ca_file must be an absolute path"},
		"ca_file a directory":   {httpsEntry(`"/etc/"`), "team team-a: ca_file must be an absolute path"},
		"ca_file not a string":  {httpsEntry(`["/etc/a.pem"]`), "team team-a: ca_file must be an absolute path"},
		"ca_file a parent step": {httpsEntry(`"/etc/../a.pem"`), "team team-a: ca_file must be a plain path"},
		"ca_file a newline":     {httpsEntry(`"/etc/a\npem"`), "team team-a: ca_file must be a plain path"},
		"another client":        {entry(`, "client": "nheko"`), "team team-a: client must be one of element, lightning"},
		"a client not a string": {entry(`, "client": null`), "team team-a: client must be one of"},
		"a team listed twice":   {list(lightningTeam("team-a"), elementTeam("team-a")), "a team is listed more than once"},
		"a secret in the url":   {`[{"team": "team-a", "base_url": "http://u:hunter2@192.0.2.10"}]`, "team team-a: base_url must carry no credentials"},
		"a secret in ca_file":   {entry(`, "ca_file": "/srv/secret-place/a.pem"`), "team team-a: ca_file"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			r := w.apply(c.stdin)
			if r.code != term.ExitConfig || r.out != "" || !strings.Contains(r.errOut, c.want) {
				t.Fatalf("%+v, want %q", r, c.want)
			}
			if strings.Contains(r.errOut, "hunter2") || strings.Contains(r.errOut, "secret-place") {
				t.Fatalf("the refusal quotes a value: %q", r.errOut)
			}
			if exists(filepath.Join(w.home, ".var")) || exists(filepath.Join(w.home, ".local")) {
				t.Fatal("a refused list wrote something")
			}
		})
	}
}

// TestL3AValidListIsAccepted holds the shapes element.py accepts: a default server_name and
// client, an IPv6 literal, https to a name with its CA file.
func TestL3AValidListIsAccepted(t *testing.T) {
	w := newWorld(t)
	stdin := list(
		`{"team": "team-a", "base_url": "`+baseURL+`"}`,
		`{"team": "team-b", "base_url": "http://[2001:db8::1]:8448", "server_name": "b.agent-bus.internal", "client": "element"}`,
		`{"team": "team-c", "base_url": "HTTPS://bus.example.com", "ca_file": "/etc/teamchat/c.pem", "client": "lightning"}`,
	)
	if r := w.run([]string{"check"}, stdin); r.code != term.ExitOK {
		t.Fatalf("%+v", r)
	}
}

// TestL4AUserHoldingPingbusBundlesIsRefused is element.py's check_user, case by case: the user
// holds pingbus bundles when ~/.config/pingbus is there in any form, except when it is a
// directory holding only a consent record pingbus would count.
func TestL4AUserHoldingPingbusBundlesIsRefused(t *testing.T) {
	const record = "accepted 2026-01-01T00:00:00Z\n"
	accepted := func(dir string) string { return filepath.Join(dir, matrixclients.AcceptedFile) }
	cases := map[string]struct {
		make func(w *world, dir string)
		want string
	}{
		"a bundle directory": {func(w *world, dir string) { mkdir(w.t, filepath.Join(dir, "team-a")) }, "holds pingbus bundles"},
		"a file":             {func(w *world, dir string) { w.write(dir, "") }, "holds pingbus bundles"},
		"a dangling symlink": {func(w *world, dir string) {
			mkdir(w.t, filepath.Dir(dir))
			symlink(w.t, filepath.Join(w.home, "nowhere"), dir)
		}, "holds pingbus bundles"},
		"the record beside a bundle": {func(w *world, dir string) {
			w.write(accepted(dir), record)
			mkdir(w.t, filepath.Join(dir, "team-a"))
		}, "holds pingbus bundles"},
		"the record beside a dotfile": {func(w *world, dir string) {
			w.write(accepted(dir), record)
			w.write(filepath.Join(dir, ".env"), "")
		}, "holds pingbus bundles"},
		"a record others can read": {func(w *world, dir string) {
			w.write(accepted(dir), record)
			chmod(w.t, accepted(dir), 0o644)
		}, "acceptance record is not valid"},
		"a record pingbus cannot read": {func(w *world, dir string) {
			w.write(accepted(dir), "clients not-an-app-id\n")
		}, "consent record pingbus can read"},
		"the record a directory": {func(w *world, dir string) { mkdir(w.t, accepted(dir)) }, "acceptance record is not valid"},
		"the record a symlink": {func(w *world, dir string) {
			w.write(filepath.Join(w.home, "real-record"), record)
			mkdir(w.t, dir)
			symlink(w.t, filepath.Join(w.home, "real-record"), accepted(dir))
		}, "acceptance record is not valid"},
		"a linked directory holding the record": {func(w *world, dir string) {
			w.write(accepted(filepath.Join(w.home, "elsewhere")), record)
			mkdir(w.t, filepath.Dir(dir))
			symlink(w.t, filepath.Join(w.home, "elsewhere"), dir)
		}, "holds pingbus bundles"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			c.make(w, filepath.Join(w.home, ".config/pingbus"))
			for _, verb := range []string{"check", "apply"} {
				r := w.run([]string{verb}, list(lightningTeam("team-a")))
				if r.code != term.ExitConfig || r.out != "" || !strings.Contains(r.errOut, c.want) {
					t.Fatalf("%s: %+v, want %q", verb, r, c.want)
				}
			}
			if exists(w.root()) {
				t.Fatal("a refused user got a Lightning tree")
			}
		})
	}
}

// TestL4TheOwnersAcceptanceAlonePasses is check_user's one exception: what cc --teams leaves,
// the owner's consent record alone, in its older form or one naming its clients; and a user with
// no pingbus directory at all.
func TestL4TheOwnersAcceptanceAlonePasses(t *testing.T) {
	for name, record := range map[string]string{
		"an older record":          "accepted 2026-01-01T00:00:00Z\n",
		"a record naming a client": "accepted 2026-01-01T00:00:00Z\nclients " + appID + "\n",
		"no pingbus directory":     "",
	} {
		t.Run(name, func(t *testing.T) {
			w := newWorld(t)
			if record == "" {
				mkdir(t, filepath.Join(w.home, ".config"))
			} else {
				w.write(filepath.Join(w.home, ".config/pingbus", matrixclients.AcceptedFile), record)
			}
			if r := w.run([]string{"check"}, "[]"); r.code != term.ExitOK {
				t.Fatalf("%+v", r)
			}
		})
	}
}

// TestL4ARecordOfAnotherUserDoesNotCount judges the record's owner by the uid the run is given,
// so no test gives a file away.
func TestL4ARecordOfAnotherUserDoesNotCount(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.home, ".config/pingbus", matrixclients.AcceptedFile), "accepted 2026-01-01T00:00:00Z\n")
	var out, errOut bytes.Buffer
	app := w.app([]string{"check"}, "[]", &out, &errOut)
	app.UID = os.Getuid() + 1
	if code := app.Run(); code != term.ExitConfig || !strings.Contains(errOut.String(), "acceptance record is not valid") {
		t.Fatalf("exit %d stderr %q", code, errOut.String())
	}
}

// TestL4AnUncheckablePingbusDirIsRefused holds that a path that cannot be looked at is a
// refusal, not a pass.
func TestL4AnUncheckablePingbusDirIsRefused(t *testing.T) {
	w := newWorld(t)
	w.write(filepath.Join(w.home, ".config"), "a file where a directory goes")
	if r := w.run([]string{"check"}, "[]"); r.code != term.ExitConfig || !strings.Contains(r.errOut, "cannot be checked for pingbus bundles") {
		t.Fatalf("%+v", r)
	}
}

// TestL5CheckWritesNothing holds that check validates and refuses only.
func TestL5CheckWritesNothing(t *testing.T) {
	w := newWorld(t)
	if r := w.run([]string{"check"}, list(lightningTeam("team-a"))); r != (result{term.ExitOK, "", ""}) {
		t.Fatalf("%+v", r)
	}
	entries, err := os.ReadDir(w.home)
	if err != nil || len(entries) != 0 {
		t.Fatalf("check wrote %v (%v)", entries, err)
	}
}

// TestL6ToL8ApplyWritesEachLightningTeam holds the directories, the settings file and the
// launcher, their modes, and the CHANGED lines naming each.
func TestL6ToL8ApplyWritesEachLightningTeam(t *testing.T) {
	w := newWorld(t)
	stdin := list(lightningTeam("team-a"), elementTeam("team-b"))
	r := w.apply(stdin)
	if r.code != term.ExitOK || r.errOut != "" {
		t.Fatalf("%+v", r)
	}
	team := w.team("team-a")
	for _, dir := range []string{filepath.Dir(team), team, team + "/data", team + "/config", team + "/cache"} {
		if got := w.mode(dir); got != 0o700 {
			t.Errorf("%s: mode %o, want 700", dir, got)
		}
	}
	if got := w.read(w.settings()); got != "[homeserver]\nurl="+baseURL+"\n" {
		t.Errorf("settings %q", got)
	}
	if got := w.mode(w.settings()); got != 0o600 {
		t.Errorf("settings mode %o", got)
	}
	w.checkLauncher()
	if exists(w.team("team-b")) || exists(w.launcher("team-b")) {
		t.Error("an Element team got a Lightning instance")
	}
	lines := strings.Split(strings.TrimSuffix(r.out, "\n"), "\n")
	for _, path := range []string{team + "/data", w.settings(), w.launcher("team-a")} {
		if !slices.Contains(lines, "CHANGED\t"+path) {
			t.Errorf("no CHANGED line for %s in %q", path, r.out)
		}
	}
	for _, line := range lines {
		if !strings.HasPrefix(line, "CHANGED\t") {
			t.Errorf("stdout line %q", line)
		}
	}
	if again := w.apply(stdin); again != (result{term.ExitOK, "", ""}) {
		t.Fatalf("a second run: %+v", again)
	}
}

// checkLauncher holds team-a's launcher to the design's text, every path absolute, mode 0644.
func (w *world) checkLauncher() {
	w.t.Helper()
	root := w.root()
	want := "[Desktop Entry]\nType=Application\nName=team-a team (Lightning)\n" +
		"Comment=Watch and command the team-a agent team\n" +
		"Exec=flatpak run --command=env " + appID +
		" XDG_DATA_HOME=" + root + "/teams/team-a/data XDG_CONFIG_HOME=" + root + "/teams/team-a/config" +
		" XDG_CACHE_HOME=" + root + "/teams/team-a/cache" +
		" SSL_CERT_FILE=/etc/ssl/certs/ca-certificates.crt SSL_CERT_DIR=" + root + "/teamchat-ca lightning-matrix\n" +
		"Icon=" + appID + "\nTerminal=false\nCategories=Network;InstantMessaging;\n"
	if got := w.read(w.launcher("team-a")); got != want {
		w.t.Errorf("launcher\n%s\nwant\n%s", got, want)
	}
	if got := w.mode(w.launcher("team-a")); got != 0o644 {
		w.t.Errorf("launcher mode %o", got)
	}
}

// TestL6AWrongModeIsPutRight holds that a directory with a wider mode is narrowed and named.
func TestL6AWrongModeIsPutRight(t *testing.T) {
	w := newWorld(t)
	w.apply(list(lightningTeam("team-a")))
	data := w.team("team-a") + "/data"
	chmod(t, data, 0o755)
	if r := w.apply(list(lightningTeam("team-a"))); r.code != term.ExitOK || r.out != "CHANGED\t"+data+"\n" || w.mode(data) != 0o700 {
		t.Fatalf("%+v", r)
	}
}

// TestL6ANonDirectoryIsRefused holds that a link where a directory goes is refused, never
// followed.
func TestL6ANonDirectoryIsRefused(t *testing.T) {
	w := newWorld(t)
	mkdir(t, filepath.Join(w.home, "elsewhere"))
	mkdir(t, filepath.Join(w.root(), "teams"))
	symlink(t, filepath.Join(w.home, "elsewhere"), w.team("team-a"))
	if r := w.apply(list(lightningTeam("team-a"))); r.code != term.ExitConfig || !strings.Contains(r.errOut, "is not a directory") {
		t.Fatalf("%+v", r)
	}
	if entries, err := os.ReadDir(filepath.Join(w.home, "elsewhere")); err != nil || len(entries) != 0 {
		t.Fatalf("the link was followed: %v %v", entries, err)
	}
}

// TestL7TheSettingsFileIsLightningsOnceWritten holds that a settings file already there is never
// rewritten, whatever it holds, and that a link there is refused.
func TestL7TheSettingsFileIsLightningsOnceWritten(t *testing.T) {
	w := newWorld(t)
	w.write(w.settings(), "[homeserver]\nurl=https://canonical.example.com\n[other]\nkey=1\n")
	if r := w.apply(list(lightningTeam("team-a"))); r.code != term.ExitOK || strings.Contains(r.out, w.settings()) {
		t.Fatalf("%+v", r)
	}
	if got := w.read(w.settings()); !strings.Contains(got, "canonical.example.com") {
		t.Fatalf("rewritten: %q", got)
	}
	linked := newWorld(t)
	mkdir(t, filepath.Dir(linked.settings()))
	symlink(t, filepath.Join(linked.home, "target"), linked.settings())
	if r := linked.apply(list(lightningTeam("team-a"))); r.code != term.ExitConfig || !strings.Contains(r.errOut, "is not a regular file") {
		t.Fatalf("a linked settings file: %+v", r)
	}
	if exists(filepath.Join(linked.home, "target")) {
		t.Fatal("the link was followed")
	}
}

// TestL8TheLauncherIsRewrittenWhenItDiffers holds that a changed or wider launcher is put back,
// and a link there refused.
func TestL8TheLauncherIsRewrittenWhenItDiffers(t *testing.T) {
	w := newWorld(t)
	stdin := list(lightningTeam("team-a"))
	w.apply(stdin)
	launcher := w.launcher("team-a")
	want := w.read(launcher)
	w.write(launcher, "[Desktop Entry]\nExec=edited\n")
	if r := w.apply(stdin); r.code != term.ExitOK || r.out != "CHANGED\t"+launcher+"\n" || w.read(launcher) != want {
		t.Fatalf("content: %+v", r)
	}
	chmod(t, launcher, 0o664)
	if r := w.apply(stdin); r.code != term.ExitOK || r.out != "CHANGED\t"+launcher+"\n" || w.mode(launcher) != 0o644 {
		t.Fatalf("mode: %+v", r)
	}
	if err := os.Remove(launcher); err != nil {
		t.Fatal(err)
	}
	symlink(t, filepath.Join(w.home, "target"), launcher)
	if r := w.apply(stdin); r.code != term.ExitConfig || !strings.Contains(r.errOut, "is not a regular file") {
		t.Fatalf("a linked launcher: %+v", r)
	}
}

// TestL9AStaleLauncherOfOursIsRemoved holds the launcher of a team declared for Element, or not
// declared at all, removed when it is recognisably this command's, and every other file kept.
func TestL9AStaleLauncherOfOursIsRemoved(t *testing.T) {
	w := newWorld(t)
	w.apply(list(lightningTeam("team-a"), lightningTeam("team-b"), lightningTeam("team-c")))
	apps := filepath.Dir(w.launcher("team-a"))
	human := filepath.Join(apps, "agent-bus-notes-lightning.desktop")
	w.write(human, "[Desktop Entry]\nExec=flatpak run "+appID+"\n")
	other := filepath.Join(apps, "other.desktop")
	w.write(other, "[Desktop Entry]\n")
	badName := filepath.Join(apps, "agent-bus-Bad_Name-lightning.desktop")
	w.write(badName, w.read(w.launcher("team-c")))
	// team-a moves to Element, team-b is not declared, team-c stays.
	r := w.apply(list(elementTeam("team-a"), lightningTeam("team-c")))
	if r.code != term.ExitOK {
		t.Fatalf("%+v", r)
	}
	if exists(w.launcher("team-a")) || exists(w.launcher("team-b")) {
		t.Fatal("a stale launcher of ours is still there")
	}
	for _, kept := range []string{w.launcher("team-c"), human, other, badName} {
		if !exists(kept) {
			t.Errorf("%s was removed", kept)
		}
	}
	for _, line := range []string{"CHANGED\t" + w.launcher("team-a"), "CHANGED\t" + w.launcher("team-b")} {
		if !strings.Contains(r.out, line+"\n") {
			t.Errorf("no %q in %q", line, r.out)
		}
	}
}

// TestL9ALinkOrPipeIsNeverOurs holds that a link with our name, even to a launcher of ours, and
// a named pipe are left alone, and the pipe is never opened.
func TestL9ALinkOrPipeIsNeverOurs(t *testing.T) {
	w := newWorld(t)
	w.apply(list(lightningTeam("team-a")))
	target := filepath.Join(w.home, "kept.desktop")
	if err := os.Rename(w.launcher("team-a"), target); err != nil {
		t.Fatal(err)
	}
	symlink(t, target, w.launcher("team-a"))
	if err := syscall.Mkfifo(w.launcher("team-b"), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := w.apply("[]"); r.code != term.ExitOK || strings.Contains(r.out, "CHANGED") {
		t.Fatalf("%+v", r)
	}
	if !exists(w.launcher("team-a")) || !exists(target) || !exists(w.launcher("team-b")) {
		t.Fatal("a link, its target or a pipe was removed")
	}
}

// TestL10ASignedInRootIsNeverRemoved holds that a team no longer declared keeps its whole
// instance, named on a KEPT line each run, and that a team moved to Element keeps it unnamed.
func TestL10ASignedInRootIsNeverRemoved(t *testing.T) {
	w := newWorld(t)
	w.apply(list(lightningTeam("team-a"), lightningTeam("team-b")))
	session := filepath.Join(w.team("team-a"), "data/MatrixClient/matrix-client/session")
	w.write(session, "signed in")
	w.write(filepath.Join(w.root(), "teams", "Not_A_Team"), "")
	for range 2 {
		r := w.apply(list(elementTeam("team-b")))
		if r.code != term.ExitOK || !strings.HasSuffix(r.out, "KEPT\t"+w.team("team-a")+"\n") ||
			strings.Contains(r.out, "KEPT\t"+w.team("team-b")) || strings.Contains(r.out, "Not_A_Team") {
			t.Fatalf("%+v", r)
		}
	}
	if !exists(session) || !exists(w.team("team-b")) {
		t.Fatal("a signed-in root was removed")
	}
}

// TestL11AFailurePrintsWhatWasDone holds that a refusal part way through still names each change
// made before it.
func TestL11AFailurePrintsWhatWasDone(t *testing.T) {
	w := newWorld(t)
	mkdir(t, filepath.Join(w.home, "elsewhere"))
	mkdir(t, filepath.Join(w.root(), "teams"))
	symlink(t, filepath.Join(w.home, "elsewhere"), w.team("team-b"))
	r := w.apply(list(lightningTeam("team-a"), lightningTeam("team-b")))
	if r.code != term.ExitConfig || !strings.Contains(r.out, "CHANGED\t"+w.launcher("team-a")+"\n") ||
		!strings.HasPrefix(r.errOut, "agent-bus-lightning: ") {
		t.Fatalf("%+v", r)
	}
}

// TestL12AHomeAnExecLineCannotHoldIsRefused holds that a home a desktop entry's Exec line would
// need to quote is refused, not guessed at, before anything is written.
func TestL12AHomeAnExecLineCannotHoldIsRefused(t *testing.T) {
	w := newWorld(t)
	w.home = filepath.Join(w.home, "a home")
	mkdir(t, w.home)
	if r := w.apply(list(lightningTeam("team-a"))); r.code != term.ExitConfig || !strings.Contains(r.errOut, "Exec line") {
		t.Fatalf("%+v", r)
	}
	if exists(filepath.Join(w.home, ".var")) {
		t.Fatal("wrote under a refused home")
	}
}

// TestL12AHomeThatCannotBeFoundFails holds the passwd lookup's failure as the run's.
func TestL12AHomeThatCannotBeFoundFails(t *testing.T) {
	w := newWorld(t)
	var out, errOut bytes.Buffer
	app := w.app([]string{"check"}, "[]", &out, &errOut)
	app.Home = func(int) (string, error) { return "", errors.New("no such user") }
	if code := app.Run(); code != term.ExitFailure || !strings.Contains(errOut.String(), "no such user") {
		t.Fatalf("exit %d stderr %q", code, errOut.String())
	}
}

// TestL13AVerdictThatCannotBeWrittenFails holds a closed stdout as exit 1.
func TestL13AVerdictThatCannotBeWrittenFails(t *testing.T) {
	w := newWorld(t)
	var errOut bytes.Buffer
	app := w.app([]string{"apply"}, list(lightningTeam("team-a")), nil, &errOut)
	app.Stdout = failingWriter{}
	if code := app.Run(); code != term.ExitFailure || !strings.Contains(errOut.String(), "writing") {
		t.Fatalf("exit %d stderr %q", code, errOut.String())
	}
}

// TestL14TheListCoversTheTeamRoots is Plan 00254 Task 5.4: the one Matrix client list names the
// folder every team's instance lives in, so the issue #251 guards see a session there.
func TestL14TheListCoversTheTeamRoots(t *testing.T) {
	l, err := matrixclients.Load()
	if err != nil {
		t.Fatal(err)
	}
	w := newWorld(t)
	w.apply(list(lightningTeam("team-a")))
	teams := filepath.Dir(w.team("team-a"))
	rel, err := filepath.Rel(w.home, teams)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Contains(l.ProfilePaths(), rel) {
		t.Fatalf("the Matrix client list's profile paths %v do not name %s", l.ProfilePaths(), rel)
	}
	found, err := l.Present(w.home)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(found, func(f matrixclients.Found) bool { return f.Path == teams }) {
		t.Fatalf("Present does not find the team roots: %v", found)
	}
}

type failingWriter struct{}

func (failingWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func mkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	mkdir(t, filepath.Dir(link))
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
}

func chmod(t *testing.T, path string, mode fs.FileMode) {
	t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
}
