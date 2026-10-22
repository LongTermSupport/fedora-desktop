// Package element is `teamchat-admin element check|apply` (Plan 00209 SPEC-n2e.md section 7,
// DESIGN-v2-C-host.md 3.10, DC18): one Element Desktop profile and one launcher per team, run as
// the desktop user. The input names teams only; each profile's homeserver URL is the team's
// approved url, read from the root-written approved.json, so an edit of the declaration cannot
// point a human's sign-in elsewhere. Every write needs the owner's recorded consent (EL4).
package element

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/status"
)

// ErrRefused is a run element refuses: as root, a bad input, no valid consent, or a target that
// is not what element writes.
var ErrRefused = errors.New("element: refused")

// ConsentText is what the owner consents to before any profile is written (C 3.10, R37). A
// changed text needs consent again (n2e-D8).
const ConsentText = "An Element profile keeps your session token in your home directory, where every " +
	"process you run can read it. Any agent run on this host as you, a host cc session included, can " +
	"read that session token and every seat of every team (the pool passwords), and so post as you " +
	"or as any member. ccy sessions are not that exposure: a container sees only what is mounted " +
	"into it, and no Element profile is."

const (
	maxInput   = 64 << 10
	maxTeams   = 64
	maxFile    = 1 << 20
	dirMode    = 0o700
	fileMode   = 0o600
	launchMode = 0o644
	appsMode   = 0o755
)

// Approvals is each team's approved fields (declare.FileApprovals); nil for a team with none.
type Approvals interface {
	Load(team paths.TeamID) (*declare.Gated, error)
}

// Names is each team's display name (n2e-D9).
type Names interface {
	Name(team paths.TeamID) (paths.DisplayName, error)
}

// StatusNames reads a team's display name from its status record at Path(team). A team with no
// record yet is named by its id.
type StatusNames struct{ Path func(paths.TeamID) string }

// Name is the record's name.
func (s StatusNames) Name(team paths.TeamID) (paths.DisplayName, error) {
	rec, found, err := status.Read(s.Path(team), team)
	if err != nil {
		return paths.DisplayName{}, err
	}
	if !found {
		return paths.ParseDisplayName(team.String())
	}
	return paths.ParseDisplayName(rec.Name)
}

// Env is what a run reads and writes: whether the process is root, the desktop user's home and
// uid (whose files the profile and the consent must be), the sources, and the streams (stdout
// carries only the CHANGED lines).
type Env struct {
	Root      bool
	Home      string
	UID       int
	Approvals Approvals
	Names     Names
	Stdout    io.Writer
	Stderr    io.Writer
}

// Check validates the input and every team's sources and writes nothing. A team not approved
// and a consent not recorded are each a line on stderr.
func Check(env Env, stdin io.Reader) error {
	teams, err := start(env, stdin)
	if err != nil {
		return err
	}
	if err := consented(env.Home, env.UID); err != nil {
		if _, werr := fmt.Fprintf(env.Stderr, "element: %v; apply would refuse\n", err); werr != nil {
			return werr
		}
	}
	for _, team := range teams {
		if _, _, err := env.profile(team); err != nil {
			return err
		}
	}
	return nil
}

// Apply writes every approved team's profile and launcher, only on change, one
// `CHANGED<TAB><path>` line for each path changed. It refuses without the owner's consent.
func Apply(env Env, stdin io.Reader) error {
	teams, err := start(env, stdin)
	if err != nil {
		return err
	}
	if err := consented(env.Home, env.UID); err != nil {
		return err
	}
	for _, team := range teams {
		p, ok, err := env.profile(team)
		if err != nil {
			return err
		}
		if !ok {
			continue
		}
		if err = env.write(team, p); err != nil {
			return err
		}
	}
	return nil
}

var errRoot = fmt.Errorf("%w: run as the human's desktop user, not root", ErrRefused)

// start refuses root (EL5) and reads the input (EL1).
func start(env Env, stdin io.Reader) ([]paths.TeamID, error) {
	if env.Root {
		return nil, errRoot
	}
	data, err := io.ReadAll(io.LimitReader(stdin, maxInput+1))
	if err != nil {
		return nil, fmt.Errorf("element: reading the input: %w", err)
	}
	return decodeTeams(data)
}

// decodeTeams is EL1: strict JSON, an array of at most maxTeams {"team": <team id>}, no team
// twice. A refusal names the entry, never its value.
func decodeTeams(data []byte) ([]paths.TeamID, error) {
	if len(data) > maxInput {
		return nil, fmt.Errorf("%w: the input is over %d bytes", ErrRefused, maxInput)
	}
	v, err := proto.Parse(data)
	if err != nil {
		return nil, fmt.Errorf("%w: the input: %w", ErrRefused, err)
	}
	if v.Kind() != proto.KindArray || len(v.Items()) > maxTeams {
		return nil, fmt.Errorf("%w: the input is not an array of at most %d teams", ErrRefused, maxTeams)
	}
	seen := map[string]bool{}
	out := make([]paths.TeamID, 0, len(v.Items()))
	for i, item := range v.Items() {
		m := item.Members()
		if item.Kind() != proto.KindObject || len(m) != 1 || m[0].Key != "team" || m[0].Val.Kind() != proto.KindString {
			return nil, fmt.Errorf("%w: entry %d is not {\"team\": <team id>} alone", ErrRefused, i+1)
		}
		team, err := paths.ParseTeamID(m[0].Val.Str())
		if err != nil {
			return nil, fmt.Errorf("%w: entry %d: the team is not a team id", ErrRefused, i+1)
		}
		if seen[team.String()] {
			return nil, fmt.Errorf("%w: entry %d: a team named twice", ErrRefused, i+1)
		}
		seen[team.String()] = true
		out = append(out, team)
	}
	return out, nil
}

// profile is what is written for a team: its approved url and display name. ok is false, and a
// line said on stderr, for a team with no approved.json.
func (env Env) profile(team paths.TeamID) (files, bool, error) {
	g, err := env.Approvals.Load(team)
	if err != nil {
		return files{}, false, fmt.Errorf("element: %s: the approved fields: %w", team, err)
	}
	if g == nil {
		_, err = fmt.Fprintf(env.Stderr, "%s: not approved yet, no profile\n", team)
		return files{}, false, err
	}
	name, err := env.Names.Name(team)
	if err != nil {
		return files{}, false, fmt.Errorf("element: %s: the display name: %w", team, err)
	}
	u, err := url.Parse(g.URL)
	if err != nil || u.Hostname() == "" {
		return files{}, false, fmt.Errorf("%w: %s: the approved url has no host", ErrRefused, team)
	}
	return files{
		config:   configJSON(g.URL, u.Hostname(), paths.ServerName(team)),
		seed:     indent(proto.Obj(proto.M("spellCheckerEnabled", proto.Bool(false)))),
		launcher: launcher(team, name),
	}, true, nil
}

// files are a team's three files' content.
type files struct {
	config, seed, launcher []byte
}

// write is EL3 for one team: the profile directory, config.json, the spell checker seed (only
// when absent) and the launcher, in that order.
func (env Env) write(team paths.TeamID, f files) error {
	dir, err := paths.ElementConfigDir(env.Home, team)
	if err != nil {
		return err
	}
	launcherPath, err := paths.ElementLauncher(env.Home, team)
	if err != nil {
		return err
	}
	changed, err := env.ensureDir(dir)
	if err != nil {
		return err
	}
	var out []string
	if changed {
		out = append(out, dir)
	}
	for _, t := range []target{
		{path: dir + "/config.json", data: f.config, mode: fileMode},
		{path: dir + "/electron-config.json", data: f.seed, mode: fileMode, onlyIfAbsent: true},
		{path: launcherPath, data: f.launcher, mode: launchMode},
	} {
		changed, err := t.put()
		if err != nil {
			return err
		}
		if changed {
			out = append(out, t.path)
		}
	}
	for _, path := range out {
		if _, err := fmt.Fprintf(env.Stdout, "CHANGED\t%s\n", path); err != nil {
			return err
		}
	}
	return nil
}

// ensureDir makes the profile directory 0700 when missing (not reported: its files are), and
// puts back the mode of one that has another (reported). Anything else there is refused.
func (env Env) ensureDir(dir string) (bool, error) {
	fi, err := os.Lstat(dir)
	if errors.Is(err, os.ErrNotExist) {
		if err = os.MkdirAll(filepath.Dir(dir), dirMode); err != nil {
			return false, err
		}
		return false, os.Mkdir(dir, dirMode)
	}
	if err != nil {
		return false, err
	}
	if !fi.IsDir() || !ownedBy(fi, env.UID) {
		return false, fmt.Errorf("%w: %s is not a directory of this user", ErrRefused, dir)
	}
	if fi.Mode().Perm() == dirMode {
		return false, nil
	}
	return true, os.Chmod(dir, dirMode)
}

// target is one file written: a temporary file and a rename, only on change; a path that is
// not a regular file is refused, never followed.
type target struct {
	path         string
	data         []byte
	mode         os.FileMode
	onlyIfAbsent bool
}

func (t target) put() (bool, error) {
	fi, err := os.Lstat(t.path)
	switch {
	case errors.Is(err, os.ErrNotExist):
		if err = os.MkdirAll(filepath.Dir(t.path), appsMode); err != nil {
			return false, err
		}
	case err != nil:
		return false, err
	case !fi.Mode().IsRegular():
		return false, fmt.Errorf("%w: %s is not a regular file", ErrRefused, t.path)
	case t.onlyIfAbsent:
		return false, nil
	default:
		old, _, err := state.ReadFile(t.path, maxFile)
		if err != nil {
			return false, err
		}
		if bytes.Equal(old, t.data) && fi.Mode().Perm() == t.mode {
			return false, nil
		}
	}
	return true, state.WriteFile(t.path, t.data, t.mode, -1)
}

// configJSON is v1's config.json: every outbound key Element would take from its bundled
// config overridden, since a profile's keys are merged over it shallowly.
func configJSON(base, host, server string) []byte {
	f := proto.Bool(false)
	t := proto.Bool(true)
	null := proto.Null()
	ui := make([]proto.Member, 0, 14)
	for _, k := range []string{
		"urlPreviews", "voip", "widgets", "locationSharing", "identityServer", "thirdPartyId", "registration",
		"passwordReset", "deactivate", "feedback", "shareSocial", "allowCreatingPublicRooms", "allowCreatingPublicSpaces",
	} {
		ui = append(ui, proto.M("UIFeature."+k, f))
	}
	ui = append(ui, proto.M("fallbackICEServerAllowed", f))
	return indent(proto.Obj(
		proto.M("default_server_config", proto.Obj(proto.M("m.homeserver", proto.Obj(
			proto.M("base_url", proto.Str(base)), proto.M("server_name", proto.Str(server)))))),
		proto.M("disable_custom_urls", t), proto.M("disable_guests", t), proto.M("disable_3pid_login", t),
		proto.M("disable_login_language_selector", t), proto.M("enable_client_well_known_lookups", f),
		proto.M("update_base_url", null), proto.M("integrations_ui_url", null), proto.M("integrations_rest_url", null),
		proto.M("integrations_widgets_urls", proto.Arr()), proto.M("bug_report_endpoint_url", null),
		proto.M("posthog", null), proto.M("sentry", null), proto.M("privacy_policy_url", null),
		proto.M("terms_and_conditions_links", proto.Arr()), proto.M("map_style_url", null),
		proto.M("jitsi", proto.Obj(proto.M("preferred_domain", proto.Str(host)))),
		proto.M("element_call", proto.Obj(proto.M("disable", t))),
		proto.M("features", proto.Obj(proto.M("feature_video_rooms", f), proto.M("feature_group_calls", f),
			proto.M("feature_element_call_video_rooms", f))),
		proto.M("room_directory", proto.Obj(proto.M("servers", proto.Arr(proto.Str(server))))),
		proto.M("show_labs_settings", f), proto.M("mobile_guide_toast", f),
		proto.M("setting_defaults", proto.Obj(ui...)),
	))
}

// indent is v's JSON, two-space indented, with a final newline.
func indent(v proto.Value) []byte {
	var b bytes.Buffer
	if err := json.Indent(&b, v.Compact(), "", "  "); err != nil {
		panic("element: the package's own JSON does not indent: " + err.Error())
	}
	b.WriteByte('\n')
	return b.Bytes()
}

// launcher is the team's desktop entry; Name= is its display name, a backslash escaped so the
// entry reads it as itself.
func launcher(team paths.TeamID, name paths.DisplayName) []byte {
	return []byte("[Desktop Entry]\nType=Application\n" +
		"Name=" + strings.ReplaceAll(paths.ElementLauncherName(name), `\`, `\\`) + "\n" +
		"Comment=Watch and command the " + team.String() + " agent team\n" +
		"Exec=flatpak run im.riot.Riot --profile " + paths.ElementProfile(team) + "\n" +
		"Icon=im.riot.Riot\nTerminal=false\nCategories=Network;InstantMessaging;\n")
}

// WriteConsent records the owner's consent to today's ConsentText at now, 0600 in a 0700
// directory env's user owns.
func WriteConsent(env Env, now time.Time) error {
	if env.Root {
		return errRoot
	}
	path, err := paths.ElementConsentFile(env.Home)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Dir(path), dirMode); err != nil {
		return err
	}
	if err = privateDir(filepath.Dir(path), env.UID); err != nil {
		return err
	}
	rec := proto.Obj(proto.M("v", proto.Int(1)), proto.M("accepted_ms", proto.Int(now.UnixMilli())),
		proto.M("text", proto.Str(ConsentText)))
	return state.WriteFile(path, append(rec.Compact(), '\n'), fileMode, -1)
}

// consented is nil when the consent record is valid (EL4): a regular file the user owns, no
// group or other bits, in a directory the user owns with none either, naming today's text.
func consented(home string, uid int) error {
	path, err := paths.ElementConsentFile(home)
	if err != nil {
		return err
	}
	if err = privateDir(filepath.Dir(path), uid); err != nil {
		return err
	}
	fi, err := os.Lstat(path)
	if err != nil {
		return fmt.Errorf("%w: no consent recorded at %s (%w)", ErrRefused, path, err)
	}
	if !fi.Mode().IsRegular() || !ownedBy(fi, uid) || fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: the consent at %s is not a regular file of this user that no one else can read", ErrRefused, path)
	}
	data, _, err := state.ReadFile(path, maxInput)
	if err != nil {
		return fmt.Errorf("%w: the consent: %w", ErrRefused, err)
	}
	if !validConsent(data) {
		return fmt.Errorf("%w: the consent at %s does not name today's consent text; record it again", ErrRefused, path)
	}
	return nil
}

// validConsent is a record of exactly v 1, accepted_ms and today's text.
func validConsent(data []byte) bool {
	v, err := proto.Parse(data)
	if err != nil || v.Kind() != proto.KindObject || len(v.Members()) != 3 {
		return false
	}
	ver, okV := v.Get("v")
	at, okAt := v.Get("accepted_ms")
	text, okText := v.Get("text")
	return okV && okAt && okText && ver.Kind() == proto.KindInt && ver.Int() == 1 &&
		at.Kind() == proto.KindInt && at.Int() >= 0 && text.Kind() == proto.KindString && text.Str() == ConsentText
}

// privateDir is nil when dir is a directory, not a link, the user owns with no group or other
// bits.
func privateDir(dir string, euid int) error {
	fi, err := os.Lstat(dir)
	if err != nil {
		return fmt.Errorf("%w: no consent recorded (%w)", ErrRefused, err)
	}
	if !fi.IsDir() || !ownedBy(fi, euid) || fi.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%w: the consent directory %s is not this user's own 0700 directory", ErrRefused, dir)
	}
	return nil
}

func ownedBy(fi os.FileInfo, euid int) bool {
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == euid
}
