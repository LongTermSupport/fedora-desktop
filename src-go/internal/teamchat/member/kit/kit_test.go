package kit

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func hostKit(t *testing.T) paths.Kit {
	t.Helper()
	d, err := paths.ParseDigest("0123456789abcdef0123456789abcdef")
	if err != nil {
		t.Fatal(err)
	}
	return paths.HostKitDir(d)
}

func emptyDir(t *testing.T) string {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "kit")
	if err := os.Mkdir(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func readJSON(t *testing.T, path string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(data, &v); err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return v
}

// KT1: the kit's tree, every mode, and the commands named by the kit's absolute path.
func TestAssembleBuildsTheKit(t *testing.T) {
	into := emptyDir(t)
	kit := hostKit(t)
	binary := []byte("\x7fELF a static teamchat")
	if err := Assemble(into, kit, bytes.NewReader(binary)); err != nil {
		t.Fatal(err)
	}
	want := map[string]fs.FileMode{
		".":                              fs.ModeDir | 0o755,
		"bin":                            fs.ModeDir | 0o755,
		"bin/teamchat":                   0o755,
		"plugin":                         fs.ModeDir | 0o755,
		"plugin/teamchat":                fs.ModeDir | 0o755,
		"plugin/teamchat/.claude-plugin": fs.ModeDir | 0o755,
		"plugin/teamchat/.claude-plugin/plugin.json": 0o644,
		"plugin/teamchat/hooks":                      fs.ModeDir | 0o755,
		"plugin/teamchat/hooks/hooks.json":           0o644,
		"settings.json":                              0o644,
	}
	got := map[string]fs.FileMode{}
	err := filepath.WalkDir(into, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(into, p)
		if err != nil {
			return err
		}
		got[rel] = info.Mode()
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != len(want) {
		t.Errorf("tree %v", got)
	}
	for p, mode := range want {
		if got[p] != mode {
			t.Errorf("%s: %v, want %v", p, got[p], mode)
		}
	}
	data, err := os.ReadFile(filepath.Join(into, "bin/teamchat"))
	if err != nil || !bytes.Equal(data, binary) {
		t.Errorf("binary %q %v", data, err)
	}
	if strings.TrimPrefix(kit.PluginHooks(), kit.String()+"/") != "plugin/teamchat/hooks/hooks.json" ||
		strings.TrimPrefix(kit.Settings(), kit.String()+"/") != "settings.json" ||
		strings.TrimPrefix(kit.Binary(), kit.String()+"/") != "bin/teamchat" {
		t.Error("the kit's tree is not paths.Kit's")
	}
}

func TestTheHooksAreTheFourEventsByAbsolutePath(t *testing.T) {
	into := emptyDir(t)
	kit := hostKit(t)
	if err := Assemble(into, kit, strings.NewReader("bin")); err != nil {
		t.Fatal(err)
	}
	doc := readJSON(t, filepath.Join(into, "plugin/teamchat/hooks/hooks.json"))
	if len(doc) != 1 {
		t.Errorf("keys %v", doc)
	}
	hooks, ok := doc["hooks"].(map[string]any)
	if !ok || len(hooks) != 4 {
		t.Fatalf("hooks %v", doc["hooks"])
	}
	for event, arg := range map[string]string{
		"SessionStart": "session-start", "UserPromptSubmit": "user-prompt-submit", "Stop": "stop", "SessionEnd": "session-end",
	} {
		want := []any{map[string]any{"hooks": []any{map[string]any{"type": "command", "command": kit.Binary() + " hook " + arg}}}}
		got, err := json.Marshal(hooks[event])
		if err != nil {
			t.Fatal(err)
		}
		wantJSON, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		if string(got) != string(wantJSON) {
			t.Errorf("%s: %s", event, got)
		}
	}
	plugin := readJSON(t, filepath.Join(into, "plugin/teamchat/.claude-plugin/plugin.json"))
	if plugin["name"] != paths.PluginName || len(plugin) != 2 {
		t.Errorf("plugin.json %v", plugin)
	}
}

// The status line at refreshInterval 25, below 30; no inbound key and no SessionEnd timeout.
func TestTheSettingsAreTheStatusLineOnly(t *testing.T) {
	into := emptyDir(t)
	kit := hostKit(t)
	if err := Assemble(into, kit, strings.NewReader("bin")); err != nil {
		t.Fatal(err)
	}
	doc := readJSON(t, filepath.Join(into, "settings.json"))
	line, ok := doc["statusLine"].(map[string]any)
	if len(doc) != 1 || !ok {
		t.Fatalf("settings %v", doc)
	}
	interval, ok := line["refreshInterval"].(float64)
	if !ok || interval != 25 || interval >= 30 {
		t.Errorf("refreshInterval %v", line["refreshInterval"])
	}
	if line["type"] != "command" || line["command"] != kit.Binary()+" status-line" || len(line) != 3 {
		t.Errorf("status line %v", line)
	}
	for _, path := range []string{"settings.json", "plugin/teamchat/hooks/hooks.json"} {
		data, err := os.ReadFile(filepath.Join(into, path))
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"crossSessionInbound", "timeout", "env", "SESSIONEND"} {
			if strings.Contains(string(data), banned) {
				t.Errorf("%s holds %s", path, banned)
			}
		}
	}
}

func TestEveryKitPathAssembles(t *testing.T) {
	for _, kit := range []paths.Kit{hostKit(t), paths.CCYKitDir(), paths.HostKitCurrent()} {
		if err := Assemble(emptyDir(t), kit, strings.NewReader("bin")); err != nil {
			t.Errorf("%s: %v", kit, err)
		}
	}
}

func TestShellSafePath(t *testing.T) {
	for p, ok := range map[string]bool{
		"/usr/local/lib/teamchat/kit/0123": true,
		"/opt/a_b-c.d":                     true,
		"/opt/a b":                         false,
		"/opt/$HOME":                       false,
		"/opt/a;b":                         false,
		"/opt/'a'":                         false,
		"/opt/a\nb":                        false,
		"relative":                         false,
		"/opt/../etc":                      false,
		"":                                 false,
		"/":                                false,
	} {
		if shellSafe(p) != ok {
			t.Errorf("%q: %v", p, !ok)
		}
	}
}

func TestAZeroKitIsRefused(t *testing.T) {
	if err := Assemble(emptyDir(t), paths.Kit{}, strings.NewReader("bin")); err == nil {
		t.Error("a zero kit assembled")
	}
}

func TestIntoMustBeAnEmptyPrivateDirectory(t *testing.T) {
	kit := hostKit(t)
	notEmpty := emptyDir(t)
	if err := os.WriteFile(filepath.Join(notEmpty, "x"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	writable := emptyDir(t)
	if err := os.Chmod(writable, 0o775); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(emptyDir(t), link); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(file, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	for name, into := range map[string]string{
		"not empty":       notEmpty,
		"group-writable":  writable,
		"a symbolic link": link,
		"a file":          file,
		"missing":         filepath.Join(t.TempDir(), "missing"),
		"relative":        "kit",
		"unclean":         notEmpty + "/../kit",
	} {
		if err := Assemble(into, kit, strings.NewReader("bin")); err == nil {
			t.Errorf("%s: assembled", name)
		}
	}
	entries, err := os.ReadDir(writable)
	if err != nil || len(entries) != 0 {
		t.Errorf("a refused directory was written: %v %v", entries, err)
	}
}

type failReader struct{}

func (failReader) Read([]byte) (int, error) { return 0, errors.New("broken") }

func TestTheBinaryMustReadWholeAndBounded(t *testing.T) {
	kit := hostKit(t)
	if err := Assemble(emptyDir(t), kit, strings.NewReader("")); err == nil {
		t.Error("an empty binary assembled")
	}
	if err := Assemble(emptyDir(t), kit, failReader{}); err == nil {
		t.Error("a failed read assembled")
	}
	saved := maxBinary
	t.Cleanup(func() { maxBinary = saved })
	maxBinary = 4
	if err := Assemble(emptyDir(t), kit, strings.NewReader("12345")); err == nil {
		t.Error("an over-cap binary assembled")
	}
	if err := Assemble(emptyDir(t), kit, io.LimitReader(strings.NewReader("12345"), 4)); err != nil {
		t.Errorf("a binary at the cap: %v", err)
	}
}

func TestHookEventsAreClaudeCodesFour(t *testing.T) {
	events := make([]string, 0, len(Hooks))
	args := make([]string, 0, len(Hooks))
	for _, h := range Hooks {
		events = append(events, h.Event)
		args = append(args, h.Arg)
	}
	if !slices.Equal(events, []string{"SessionStart", "UserPromptSubmit", "Stop", "SessionEnd"}) ||
		!slices.Equal(args, []string{"session-start", "user-prompt-submit", "stop", "session-end"}) {
		t.Errorf("%v %v", events, args)
	}
}

func TestBuildStopsAtTheFirstFailure(t *testing.T) {
	for name, list := range map[string][]entry{
		"a directory twice": {{"a", dirMode, nil, 0}, {"a", dirMode, nil, 0}},
		"a file twice":      {{"f", fileMode, strings.NewReader("x"), 8}, {"f", fileMode, strings.NewReader("y"), 8}},
	} {
		root, err := openRoot(emptyDir(t))
		if err != nil {
			t.Fatal(err)
		}
		if err := errors.Join(build(root, list), root.close()); err == nil {
			t.Errorf("%s: built", name)
		}
	}
}
