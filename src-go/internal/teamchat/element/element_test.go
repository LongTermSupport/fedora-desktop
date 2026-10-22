package element

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

var testNow = time.UnixMilli(1_800_000_000_000)

func mustTeam(t *testing.T, s string) paths.TeamID {
	t.Helper()
	team, err := paths.ParseTeamID(s)
	if err != nil {
		t.Fatal(err)
	}
	return team
}

// approvals is a fake of the teams' approved.json: a team in urls is approved with that url.
type approvals struct {
	urls map[string]string
	err  error
}

func (a approvals) Load(team paths.TeamID) (*declare.Gated, error) {
	u, ok := a.urls[team.String()]
	if !ok || a.err != nil {
		return nil, a.err
	}
	return &declare.Gated{URL: u}, nil
}

// names is a fake of the status records' names.
type names struct {
	byTeam map[string]string
	err    error
}

func (n names) Name(team paths.TeamID) (paths.DisplayName, error) {
	if n.err != nil {
		return paths.DisplayName{}, n.err
	}
	s, ok := n.byTeam[team.String()]
	if !ok {
		s = team.String()
	}
	return paths.ParseDisplayName(s)
}

// setup is a desktop user's home with consent given, and the env over it.
type setup struct {
	t      *testing.T
	home   string
	env    Env
	stdout *bytes.Buffer
	stderr *bytes.Buffer
}

func newSetup(t *testing.T) *setup {
	t.Helper()
	s := &setup{t: t, home: t.TempDir(), stdout: &bytes.Buffer{}, stderr: &bytes.Buffer{}}
	s.env = Env{
		Home: s.home, UID: os.Geteuid(),
		Approvals: approvals{urls: map[string]string{"ops": "http://192.0.2.10:8448", "web": "https://[2001:db8::1]:8448"}},
		Names:     names{byTeam: map[string]string{"ops": "Operations"}},
		Stdout:    s.stdout, Stderr: s.stderr,
	}
	if err := WriteConsent(s.env, testNow); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *setup) apply(input string) error {
	s.t.Helper()
	s.stdout.Reset()
	s.stderr.Reset()
	return Apply(s.env, strings.NewReader(input))
}

func (s *setup) check(input string) error {
	s.t.Helper()
	s.stdout.Reset()
	s.stderr.Reset()
	return Check(s.env, strings.NewReader(input))
}

func (s *setup) profile(team string) string {
	s.t.Helper()
	dir, err := paths.ElementConfigDir(s.home, mustTeam(s.t, team))
	if err != nil {
		s.t.Fatal(err)
	}
	return dir
}

func (s *setup) launcher(team string) string {
	s.t.Helper()
	path, err := paths.ElementLauncher(s.home, mustTeam(s.t, team))
	if err != nil {
		s.t.Fatal(err)
	}
	return path
}

func (s *setup) changed() []string {
	var out []string
	for line := range strings.Lines(s.stdout.String()) {
		path, ok := strings.CutPrefix(strings.TrimSuffix(line, "\n"), "CHANGED\t")
		if !ok {
			s.t.Errorf("a stdout line that is not CHANGED: %q", line)
		}
		out = append(out, path)
	}
	return out
}

func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func perm(t *testing.T, path string) os.FileMode {
	t.Helper()
	fi, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

// EL1: the input is strict: an array of {"team": <team id>} and nothing else.
func TestInputIsStrict(t *testing.T) {
	many := "[" + strings.Repeat(`{"team":"ops"},`, 64) + `{"team":"ops"}]`
	for name, input := range map[string]string{
		"not json":       `team ops`,
		"an object":      `{"team":"ops"}`,
		"a base_url":     `[{"team":"ops","base_url":"http://192.0.2.66:8448"}]`,
		"no team":        `[{}]`,
		"a bad team":     `[{"team":"Ops"}]`,
		"a reserved one": `[{"team":"admin"}]`,
		"a number":       `[{"team":7}]`,
		"a repeated key": `[{"team":"ops","team":"web"}]`,
		"a repeated one": `[{"team":"ops"},{"team":"ops"}]`,
		"trailing data":  `[{"team":"ops"}] []`,
		"not an object":  `["ops"]`,
		"over 64":        many,
	} {
		t.Run(name, func(t *testing.T) {
			s := newSetup(t)
			if err := s.check(input); !errors.Is(err, ErrRefused) {
				t.Errorf("check: %v", err)
			}
			if err := s.apply(input); !errors.Is(err, ErrRefused) {
				t.Errorf("apply: %v", err)
			}
			if strings.Contains(s.stderr.String()+s.stdout.String(), "192.0.2.66") {
				t.Errorf("a refused value was shown")
			}
		})
	}
}

// EL2, EL3: apply writes each approved team's profile from its approved url and its status
// record's name, then on a second run changes nothing.
func TestApplyWritesTheProfiles(t *testing.T) {
	s := newSetup(t)
	if err := s.apply(`[{"team":"ops"},{"team":"web"}]`); err != nil {
		t.Fatal(err)
	}
	want := []string{
		s.profile("ops") + "/config.json", s.profile("ops") + "/electron-config.json", s.launcher("ops"),
		s.profile("web") + "/config.json", s.profile("web") + "/electron-config.json", s.launcher("web"),
	}
	if got := s.changed(); !slices.Equal(got, want) {
		t.Errorf("changed %v, want %v", got, want)
	}
	if perm(t, s.profile("ops")) != 0o700 || perm(t, want[0]) != 0o600 || perm(t, want[1]) != 0o600 || perm(t, want[2]) != 0o644 {
		t.Errorf("modes %v %v %v %v", perm(t, s.profile("ops")), perm(t, want[0]), perm(t, want[1]), perm(t, want[2]))
	}
	if got := read(t, want[1]); got != "{\n  \"spellCheckerEnabled\": false\n}\n" {
		t.Errorf("electron-config.json %q", got)
	}
	wantLauncher := "[Desktop Entry]\nType=Application\nName=Operations team (teamchat)\n" +
		"Comment=Watch and command the ops agent team\nExec=flatpak run im.riot.Riot --profile teamchat-ops\n" +
		"Icon=im.riot.Riot\nTerminal=false\nCategories=Network;InstantMessaging;\n"
	if got := read(t, want[2]); got != wantLauncher {
		t.Errorf("launcher %q", got)
	}
	if got := read(t, want[5]); !strings.Contains(got, "\nName=web team (teamchat)\n") {
		t.Errorf("a team with no name in its record: %q", got)
	}
	if err := s.apply(`[{"team":"ops"},{"team":"web"}]`); err != nil || s.stdout.Len() != 0 {
		t.Errorf("a second run: %v, %q", err, s.stdout.String())
	}
}

// EL3: config.json is v1's key set, every outbound key overridden, on the approved url, the
// team's server name, and the homeserver's host as the Jitsi domain (an IPv6 one unbracketed).
func TestConfigKeys(t *testing.T) {
	s := newSetup(t)
	if err := s.apply(`[{"team":"ops"},{"team":"web"}]`); err != nil {
		t.Fatal(err)
	}
	var cfg map[string]any
	if err := json.Unmarshal([]byte(read(t, s.profile("web")+"/config.json")), &cfg); err != nil {
		t.Fatal(err)
	}
	server := paths.ServerName(mustTeam(t, "web"))
	hs := map[string]any{"m.homeserver": map[string]any{"base_url": "https://[2001:db8::1]:8448", "server_name": server}}
	want := map[string]any{
		"default_server_config": hs, "disable_custom_urls": true, "disable_guests": true,
		"disable_3pid_login": true, "disable_login_language_selector": true,
		"enable_client_well_known_lookups": false, "update_base_url": nil, "integrations_ui_url": nil,
		"integrations_rest_url": nil, "integrations_widgets_urls": []any{}, "bug_report_endpoint_url": nil,
		"posthog": nil, "sentry": nil, "privacy_policy_url": nil, "terms_and_conditions_links": []any{},
		"map_style_url": nil, "jitsi": map[string]any{"preferred_domain": "2001:db8::1"},
		"element_call": map[string]any{"disable": true},
		"features": map[string]any{
			"feature_video_rooms": false, "feature_group_calls": false, "feature_element_call_video_rooms": false,
		},
		"room_directory": map[string]any{"servers": []any{server}}, "show_labs_settings": false,
		"mobile_guide_toast": false, "setting_defaults": map[string]any{
			"UIFeature.urlPreviews": false, "UIFeature.voip": false, "UIFeature.widgets": false,
			"UIFeature.locationSharing": false, "UIFeature.identityServer": false,
			"UIFeature.thirdPartyId": false, "UIFeature.registration": false,
			"UIFeature.passwordReset": false, "UIFeature.deactivate": false, "UIFeature.feedback": false,
			"UIFeature.shareSocial": false, "UIFeature.allowCreatingPublicRooms": false,
			"UIFeature.allowCreatingPublicSpaces": false, "fallbackICEServerAllowed": false,
		},
	}
	got, err := json.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	wantJSON, err := json.Marshal(want)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(wantJSON) {
		t.Errorf("config.json\n got %s\nwant %s", got, wantJSON)
	}
	if text := read(t, s.profile("ops")+"/config.json"); !strings.HasSuffix(text, "}\n") || !strings.Contains(text, "\n  \"jitsi\": {\n    \"preferred_domain\": \"192.0.2.10\"") {
		t.Errorf("ops config.json %q", text)
	}
}

// EL3: a changed url or name rewrites only its file; a seeded spell checker the user changed is
// kept; a profile directory with another mode is put back and reported.
func TestApplyOnlyOnChange(t *testing.T) {
	s := newSetup(t)
	if err := s.apply(`[{"team":"ops"}]`); err != nil {
		t.Fatal(err)
	}
	seed := s.profile("ops") + "/electron-config.json"
	if err := os.WriteFile(seed, []byte(`{"spellCheckerEnabled":true}`), 0o600); err != nil {
		t.Fatal(err)
	}
	s.env.Approvals = approvals{urls: map[string]string{"ops": "http://192.0.2.11:8448"}}
	if err := s.apply(`[{"team":"ops"}]`); err != nil {
		t.Fatal(err)
	}
	if got := s.changed(); !slices.Equal(got, []string{s.profile("ops") + "/config.json"}) {
		t.Errorf("a changed url: %v", got)
	}
	if read(t, seed) != `{"spellCheckerEnabled":true}` {
		t.Errorf("the user's spell checker choice was overwritten")
	}
	s.env.Names = names{byTeam: map[string]string{"ops": "Ops Desk"}}
	if err := os.Chmod(s.profile("ops"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(`[{"team":"ops"}]`); err != nil {
		t.Fatal(err)
	}
	if got := s.changed(); !slices.Equal(got, []string{s.profile("ops"), s.launcher("ops")}) {
		t.Errorf("a changed name and mode: %v", got)
	}
	if perm(t, s.profile("ops")) != 0o700 || !strings.Contains(read(t, s.launcher("ops")), "\nName=Ops Desk team (teamchat)\n") {
		t.Errorf("not put back")
	}
	if err := os.Chmod(s.launcher("ops"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(`[{"team":"ops"}]`); err != nil || !slices.Equal(s.changed(), []string{s.launcher("ops")}) || perm(t, s.launcher("ops")) != 0o644 {
		t.Errorf("a changed mode alone: %v, %v", err, s.changed())
	}
}

// EL3: a display name's backslash is escaped in the launcher, so it reads as itself.
func TestLauncherEscapesTheName(t *testing.T) {
	s := newSetup(t)
	s.env.Names = names{byTeam: map[string]string{"ops": `A\nB`}}
	if err := s.apply(`[{"team":"ops"}]`); err != nil {
		t.Fatal(err)
	}
	if got := read(t, s.launcher("ops")); !strings.Contains(got, "\nName=A\\\\nB team (teamchat)\n") {
		t.Errorf("launcher %q", got)
	}
}

// EL2: a team with no approved.json gets no profile, said on stderr by check and apply; an
// approvals or name read that fails is the error.
func TestUnapprovedTeam(t *testing.T) {
	s := newSetup(t)
	if err := s.check(`[{"team":"dev"}]`); err != nil || s.stderr.String() != "dev: not approved yet, no profile\n" {
		t.Errorf("check: %v, %q", err, s.stderr.String())
	}
	if err := s.apply(`[{"team":"dev"},{"team":"ops"}]`); err != nil || s.stderr.String() != "dev: not approved yet, no profile\n" {
		t.Errorf("apply: %v, %q", err, s.stderr.String())
	}
	if exists(t, s.profile("dev")) || exists(t, s.launcher("dev")) || !exists(t, s.launcher("ops")) {
		t.Errorf("profiles written: dev %v, ops %v", exists(t, s.profile("dev")), exists(t, s.launcher("ops")))
	}
	boom := errors.New("boom")
	s.env.Approvals = approvals{err: boom}
	if err := s.apply(`[{"team":"ops"}]`); !errors.Is(err, boom) {
		t.Errorf("approvals: %v", err)
	}
	if err := s.check(`[{"team":"ops"}]`); !errors.Is(err, boom) {
		t.Errorf("check approvals: %v", err)
	}
	s.env.Approvals, s.env.Names = approvals{urls: map[string]string{"ops": "http://192.0.2.10:8448"}}, names{err: boom}
	if err := s.apply(`[{"team":"ops"}]`); !errors.Is(err, boom) {
		t.Errorf("names: %v", err)
	}
}

func exists(t *testing.T, path string) bool {
	t.Helper()
	_, err := os.Lstat(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		t.Fatal(err)
	}
	return err == nil
}

// EL3: anything at a target that is not a regular file, or at the profile directory that is
// not a directory, is refused and never followed.
func TestApplyRefusesWhatIsNotRegular(t *testing.T) {
	for name, plant := range map[string]func(s *setup){
		"a linked config": func(s *setup) { s.link(s.profile("ops") + "/config.json") },
		"a linked seed":   func(s *setup) { s.link(s.profile("ops") + "/electron-config.json") },
		"a linked launcher": func(s *setup) {
			s.link(s.launcher("ops"))
		},
		"a directory as config": func(s *setup) { s.mkdir(s.profile("ops") + "/config.json") },
		"a linked profile":      func(s *setup) { s.link(s.profile("ops")) },
		"a file as profile": func(s *setup) {
			s.mkdir(filepath.Dir(s.profile("ops")))
			if err := os.WriteFile(s.profile("ops"), nil, 0o600); err != nil {
				s.t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSetup(t)
			plant(s)
			if err := s.apply(`[{"team":"ops"}]`); !errors.Is(err, ErrRefused) {
				t.Errorf("apply: %v", err)
			}
			if exists(t, filepath.Join(s.home, "elsewhere", "config.json")) {
				t.Errorf("a link was followed")
			}
		})
	}
}

func (s *setup) mkdir(path string) {
	s.t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		s.t.Fatal(err)
	}
}

// link makes path a link to the directory elsewhere in the home.
func (s *setup) link(path string) {
	s.t.Helper()
	s.mkdir(filepath.Join(s.home, "elsewhere"))
	s.mkdir(filepath.Dir(path))
	if err := os.Symlink(filepath.Join(s.home, "elsewhere"), path); err != nil {
		s.t.Fatal(err)
	}
}

// EL4: apply refuses without a valid consent record naming today's text; check writes nothing
// and says the consent is missing on stderr.
func TestConsent(t *testing.T) {
	consent, err := paths.ElementConsentFile("/h")
	if err != nil {
		t.Fatal(err)
	}
	rel := strings.TrimPrefix(consent, "/h/")
	for name, spoil := range map[string]func(s *setup, path string){
		"missing": func(s *setup, path string) {
			if err := os.Remove(path); err != nil {
				s.t.Fatal(err)
			}
		},
		"group readable": func(s *setup, path string) { s.chmod(path, 0o640) },
		"an open directory": func(s *setup, path string) {
			s.chmod(filepath.Dir(path), 0o755)
		},
		"another text": func(s *setup, path string) {
			s.rewrite(path, `{"v":1,"accepted_ms":1,"text":"an older consent text"}`)
		},
		"another version": func(s *setup, path string) {
			s.rewrite(path, `{"v":2,"accepted_ms":1,"text":`+quote(s.t, ConsentText)+`}`)
		},
		"an unknown key": func(s *setup, path string) {
			s.rewrite(path, `{"v":1,"accepted_ms":1,"text":`+quote(s.t, ConsentText)+`,"x":1}`)
		},
		"v1's record": func(s *setup, path string) { s.rewrite(path, "accepted 2026-01-01T00:00:00Z\n") },
		"a link": func(s *setup, path string) {
			if err := os.Rename(path, path+".real"); err != nil {
				s.t.Fatal(err)
			}
			if err := os.Symlink(path+".real", path); err != nil {
				s.t.Fatal(err)
			}
		},
		"a linked directory": func(s *setup, path string) {
			dir := filepath.Dir(path)
			if err := os.Rename(dir, dir+".real"); err != nil {
				s.t.Fatal(err)
			}
			if err := os.Symlink(dir+".real", dir); err != nil {
				s.t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := newSetup(t)
			spoil(s, filepath.Join(s.home, rel))
			if err := s.apply(`[{"team":"ops"}]`); !errors.Is(err, ErrRefused) || exists(t, s.profile("ops")) {
				t.Errorf("apply: %v", err)
			}
			if err := s.check(`[{"team":"ops"}]`); err != nil || !strings.Contains(s.stderr.String(), "consent") {
				t.Errorf("check: %v, %q", err, s.stderr.String())
			}
		})
	}
}

func (s *setup) chmod(path string, mode os.FileMode) {
	s.t.Helper()
	if err := os.Chmod(path, mode); err != nil {
		s.t.Fatal(err)
	}
}

func (s *setup) rewrite(path, data string) {
	s.t.Helper()
	if err := os.WriteFile(path, []byte(data), 0o600); err != nil {
		s.t.Fatal(err)
	}
}

func quote(t *testing.T, s string) string {
	t.Helper()
	b, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// EL4: WriteConsent writes the record 0600 in a 0700 directory, naming the text and the time;
// another owner's uid is never given consent.
func TestWriteConsent(t *testing.T) {
	s := newSetup(t)
	path, err := paths.ElementConsentFile(s.home)
	if err != nil {
		t.Fatal(err)
	}
	if perm(t, path) != 0o600 || perm(t, filepath.Dir(path)) != 0o700 {
		t.Errorf("modes %v %v", perm(t, path), perm(t, filepath.Dir(path)))
	}
	want := `{"v":1,"accepted_ms":1800000000000,"text":` + quote(t, ConsentText) + "}\n"
	if got := read(t, path); got != want {
		t.Errorf("record %q", got)
	}
	if err := s.check(`[{"team":"ops"}]`); err != nil || s.stderr.Len() != 0 || exists(t, s.profile("ops")) {
		t.Errorf("check with consent: %v, %q", err, s.stderr.String())
	}
	s.env.UID++
	if err := WriteConsent(s.env, testNow); err == nil {
		t.Errorf("consent written for another uid")
	}
	if err := s.apply(`[{"team":"ops"}]`); !errors.Is(err, ErrRefused) {
		t.Errorf("another owner's record: %v", err)
	}
	if err := WriteConsent(Env{Home: "relative"}, testNow); err == nil {
		t.Errorf("a relative home")
	}
}

// EL4: the consent text says plainly what a host session can read.
func TestConsentText(t *testing.T) {
	for _, want := range []string{"cc", "session token", "every seat", "ccy"} {
		if !strings.Contains(ConsentText, want) {
			t.Errorf("the consent text does not name %q", want)
		}
	}
}

// failing is a stream that cannot be written.
type failing struct{}

var errStream = errors.New("the stream is closed")

func (failing) Write([]byte) (int, error) { return 0, errStream }

// A url with no host, an input over its cap, a launcher directory that cannot be made and a
// stream that cannot be written are each the run's error.
func TestApplyErrors(t *testing.T) {
	s := newSetup(t)
	s.env.Approvals = approvals{urls: map[string]string{"ops": "http:opaque"}}
	if err := s.apply(`[{"team":"ops"}]`); !errors.Is(err, ErrRefused) {
		t.Errorf("a url with no host: %v", err)
	}
	if err := s.apply("[" + strings.Repeat(" ", 70<<10) + "]"); !errors.Is(err, ErrRefused) {
		t.Errorf("an input over its cap: %v", err)
	}
	s = newSetup(t)
	s.env.Stdout = failing{}
	if err := s.apply(`[{"team":"ops"}]`); !errors.Is(err, errStream) {
		t.Errorf("stdout: %v", err)
	}
	s = newSetup(t)
	s.env.Stderr = failing{}
	if err := s.apply(`[{"team":"dev"}]`); !errors.Is(err, errStream) {
		t.Errorf("stderr: %v", err)
	}
	if err := os.Remove(filepath.Join(s.home, ".config", "teamchat", "element-consent")); err != nil {
		t.Fatal(err)
	}
	if err := s.check(`[{"team":"ops"}]`); !errors.Is(err, errStream) {
		t.Errorf("check's stderr: %v", err)
	}
	s = newSetup(t)
	s.mkdir(filepath.Join(s.home, ".local"))
	if err := os.WriteFile(filepath.Join(s.home, ".local", "share"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(`[{"team":"ops"}]`); err == nil {
		t.Errorf("a launcher directory that cannot be made was taken")
	}
}

// EL4: a consent that is a directory is no consent.
func TestConsentThatIsADirectory(t *testing.T) {
	s := newSetup(t)
	path := filepath.Join(s.home, ".config", "teamchat", "element-consent")
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	s.mkdir(path)
	if err := s.apply(`[{"team":"ops"}]`); !errors.Is(err, ErrRefused) {
		t.Errorf("apply: %v", err)
	}
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Dir(path)); err != nil {
		t.Fatal(err)
	}
	if err := s.apply(`[{"team":"ops"}]`); !errors.Is(err, ErrRefused) {
		t.Errorf("no consent directory: %v", err)
	}
}

// EL5: element refuses to run as root.
func TestRefusesRoot(t *testing.T) {
	s := newSetup(t)
	s.env.Root = true
	if err := s.check(`[{"team":"ops"}]`); !errors.Is(err, ErrRefused) {
		t.Errorf("check: %v", err)
	}
	if err := s.apply(`[{"team":"ops"}]`); !errors.Is(err, ErrRefused) {
		t.Errorf("apply: %v", err)
	}
	if err := WriteConsent(s.env, testNow); !errors.Is(err, ErrRefused) {
		t.Errorf("consent: %v", err)
	}
}

// EL2: StatusNames is the status record's name; a team with no record is named by its id, and
// a record that does not read is the error.
func TestStatusNames(t *testing.T) {
	dir := t.TempDir()
	ns := StatusNames{Path: func(team paths.TeamID) string { return filepath.Join(dir, team.String()+".json") }}
	got, err := ns.Name(mustTeam(t, "ops"))
	if err != nil || got.String() != "ops" {
		t.Errorf("no record: %v, %v", got, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "web.json"), []byte("{}"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ns.Name(mustTeam(t, "web")); err == nil {
		t.Errorf("a record that does not decode was read")
	}
	rec := status.Next(nil, mustTeam(t, "dev"), testNow)
	rec.Name, rec.Origin = "Dev Desk", "v2"
	rec.Homeserver = status.Homeserver{Unit: "active", Probe: "ok"}
	rec.Backup.LastRun = "none"
	if err := status.Write(filepath.Join(dir, "dev.json"), rec, -1); err != nil {
		t.Fatal(err)
	}
	if got, err := ns.Name(mustTeam(t, "dev")); err != nil || got.String() != "Dev Desk" {
		t.Errorf("a record: %v, %v", got, err)
	}
	rec.Name = "a\tb"
	if err := status.Write(filepath.Join(dir, "dev.json"), rec, -1); err == nil {
		if _, err := ns.Name(mustTeam(t, "dev")); err == nil {
			t.Errorf("a name that is not a display name was taken")
		}
	}
}
