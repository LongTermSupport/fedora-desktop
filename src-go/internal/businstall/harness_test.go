package businstall

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
)

// The host the installer runs on, simulated: stubs for every program the installer runs, with
// the state they keep, so a flow can be run end to end in a temporary prefix. It mirrors the
// stubs of scripts/test-agent-bus-install.bash, which stay the oracle.

type unitState struct{ active, enabled, failed bool }

type failRule struct {
	prefix string
	code   int
}

type sim struct {
	prefix    string
	units     map[string]*unitState
	failStart map[string]bool
	packages  map[string]bool
	users     map[string]bool
	asset     []byte
	zones     map[string]string
	routeDev  string
	ipAddr    string
	ssAddrs   []string
	fwPerm    map[string]bool
	fwRun     map[string]bool
	fwFailAdd string
	nmAddrs   string
	nmExists  bool
	nmUp      bool
	machine   string
	fails     []failRule
	log       []string
	bootstrap string
	// renderRefuse, when not 0, is the exit status of `render check`.
	renderRefuse int
	bundleBad    string
}

func newSim(prefix string, asset []byte) *sim {
	return &sim{
		prefix:    prefix,
		units:     map[string]*unitState{},
		failStart: map[string]bool{},
		packages:  map[string]bool{},
		users:     map[string]bool{},
		asset:     asset,
		zones:     map[string]string{},
		routeDev:  "eth0",
		ipAddr: `[{"ifname":"lo","addr_info":[{"local":"127.0.0.1"}]},` +
			`{"ifname":"agentbus0","addr_info":[{"local":"192.0.2.10"}]},` +
			`{"ifname":"wg0","addr_info":[{"local":"2001:db8::1"}]}]`,
		ssAddrs: []string{"127.0.0.1", "192.0.2.10"},
		fwPerm:  map[string]bool{},
		fwRun:   map[string]bool{},
		machine: "x86_64",
	}
}

func (s *sim) unit(name string) *unitState {
	if s.units[name] == nil {
		s.units[name] = &unitState{}
	}
	return s.units[name]
}

func (s *sim) anyActive(prefix string) bool {
	for name, u := range s.units {
		if strings.HasPrefix(name, prefix) && u.active {
			return true
		}
	}
	return false
}

func (s *sim) lines(re string) int {
	rx := regexp.MustCompile(re)
	n := 0
	for _, l := range s.log {
		if rx.MatchString(l) {
			n++
		}
	}
	return n
}

func ok(stdout string) (execx.Result, error) { return execx.Result{Stdout: []byte(stdout)}, nil }

func refuse(code int, stderr string) (execx.Result, error) {
	return execx.Result{ExitCode: code, Stderr: []byte(stderr)}, nil
}

func (s *sim) answer(c execx.Cmd) (execx.Result, error) {
	line := c.Line()
	s.log = append(s.log, line)
	for _, f := range s.fails {
		if strings.HasPrefix(line, f.prefix) {
			return refuse(f.code, "injected failure of "+f.prefix)
		}
	}
	tools := map[string]func(execx.Cmd) (execx.Result, error){
		"systemctl":   func(c execx.Cmd) (execx.Result, error) { return s.systemctl(c.Args) },
		"systemd-run": func(c execx.Cmd) (execx.Result, error) { return s.systemdRun(c.Args) },
		"journalctl": func(execx.Cmd) (execx.Result, error) {
			return ok("stub journal: tuwunel exited, status=1/FAILURE\n")
		},
		"firewall-cmd":             func(c execx.Cmd) (execx.Result, error) { return s.firewall(c.Args) },
		"nmcli":                    func(c execx.Cmd) (execx.Result, error) { return s.nmcli(c.Args) },
		"curl":                     func(c execx.Cmd) (execx.Result, error) { return s.curl(c.Args) },
		"ip":                       s.ip,
		"ss":                       func(c execx.Cmd) (execx.Result, error) { return s.ss(c.Args) },
		"rpm":                      s.rpm,
		"dnf":                      s.dnf,
		"getent":                   s.getent,
		"useradd":                  s.useradd,
		"zstd":                     func(c execx.Cmd) (execx.Result, error) { return s.zstd(c.Args) },
		"uname":                    func(execx.Cmd) (execx.Result, error) { return ok(s.machine + "\n") },
		"python3":                  func(c execx.Cmd) (execx.Result, error) { return s.bundler(c.Args) },
		"agent-bus":                s.adminTool,
		"/usr/local/bin/agent-bus": s.adminTool,
	}
	if tool, found := tools[c.Name]; found {
		return tool(c)
	}
	return execx.Result{}, fmt.Errorf("%w: %s", execx.ErrUnexpected, line)
}

func (s *sim) ip(c execx.Cmd) (execx.Result, error) {
	if slices.Equal(c.Args, []string{"-j", "addr", "show"}) {
		return ok(s.ipAddr)
	}
	return ok(fmt.Sprintf(`[{"dst":%q,"dev":%q}]`, c.Args[3], s.routeDev))
}

func (s *sim) rpm(c execx.Cmd) (execx.Result, error) {
	if s.packages[c.Args[2]] {
		return ok("")
	}
	return refuse(1, "")
}

func (s *sim) dnf(c execx.Cmd) (execx.Result, error) {
	for _, a := range c.Args {
		if a != "install" && !strings.HasPrefix(a, "-") {
			s.packages[a] = true
		}
	}
	return ok("Complete!\n")
}

func (s *sim) getent(c execx.Cmd) (execx.Result, error) {
	if s.users[c.Args[1]] {
		return ok(c.Args[1] + ":x:990:990::/var/lib/agent-bus:/usr/sbin/nologin\n")
	}
	return refuse(2, "")
}

func (s *sim) useradd(c execx.Cmd) (execx.Result, error) {
	s.users[c.Args[len(c.Args)-1]] = true
	return ok("")
}

func (s *sim) systemctl(argv []string) (execx.Result, error) {
	cmd, rest := argv[0], argv[1:]
	var args []string
	for _, a := range rest {
		if !strings.HasPrefix(a, "--") {
			args = append(args, a)
		}
	}
	switch cmd {
	case "daemon-reload":
		return ok("")
	case "enable", "disable", "start", "restart", "stop":
		return s.changeUnits(cmd, args, slices.Contains(rest, "--now"))
	case "is-enabled":
		if s.unit(args[0]).enabled {
			return ok("")
		}
		return refuse(1, "")
	case "kill":
		return s.signal(args[0])
	case "show":
		return s.show(args)
	}
	return refuse(99, "systemctl stub: unexpected "+cmd)
}

// changeUnits is the verbs of systemctl that change a unit's state.
func (s *sim) changeUnits(cmd string, units []string, now bool) (execx.Result, error) {
	for _, u := range units {
		st := s.unit(u)
		switch cmd {
		case "enable":
			st.enabled = true
		case "disable":
			st.enabled = false
			st.active = st.active && !now
		case "start", "restart":
			st.active, st.failed = !s.failStart[u], s.failStart[u]
		case "stop":
			st.active = false
		}
	}
	if cmd == "enable" {
		return ok("Created symlink\n")
	}
	return ok("")
}

// signal is SIGUSR2 to the homeserver: it writes the next numbered backup.
func (s *sim) signal(unit string) (execx.Result, error) {
	team := strings.TrimSuffix(strings.TrimPrefix(unit, "agent-bus-hs@"), ".service")
	meta := filepath.Join(s.prefix, "var/lib/agent-bus", team, "backups/meta")
	if err := os.MkdirAll(meta, 0o750); err != nil {
		return execx.Result{}, err
	}
	entries, err := os.ReadDir(meta)
	if err != nil {
		return execx.Result{}, err
	}
	highest := 0
	for _, e := range entries {
		var n int
		if _, scanErr := fmt.Sscanf(e.Name(), "%d", &n); scanErr == nil && n > highest {
			highest = n
		}
	}
	if err := os.Mkdir(filepath.Join(meta, strconv.Itoa(highest+1)), 0o750); err != nil {
		return execx.Result{}, err
	}
	return ok("")
}

func (s *sim) show(args []string) (execx.Result, error) {
	prop, name := args[1], args[2]
	u := s.unit(name)
	switch prop {
	case "ActiveState":
		switch {
		case u.active:
			return ok("active\n")
		case u.failed:
			return ok("failed\n")
		}
		return ok("inactive\n")
	case "SubState":
		switch {
		case u.active:
			return ok("running\n")
		case u.failed:
			return ok("auto-restart\n")
		}
		return ok("dead\n")
	case "UnitFileState":
		if u.enabled {
			return ok("enabled\n")
		}
		return ok("disabled\n")
	case "SocketBindDeny":
		return ok("any\nany\n")
	}
	return ok("stub-" + prop + "\n")
}

func (s *sim) systemdRun(argv []string) (execx.Result, error) {
	var unit string
	collect := false
	for _, a := range argv {
		if v, found := strings.CutPrefix(a, "--unit="); found {
			unit = v
		}
		collect = collect || a == "--collect"
	}
	if u := s.units[unit]; u != nil && (u.active || u.failed) {
		return refuse(1, "Failed to start transient service unit: Unit "+unit+" was already loaded")
	}
	if s.failStart[unit] {
		if !collect {
			s.unit(unit).failed = true
		}
	} else {
		s.unit(unit).active = true
	}
	return ok("Running as unit: " + unit + "\n")
}

func (s *sim) firewall(argv []string) (execx.Result, error) {
	set := s.fwRun
	var zone, op, rule string
	for _, a := range argv {
		if res, answered := s.zoneQuery(a); answered {
			return res, nil
		}
		switch {
		case a == "--permanent":
			set = s.fwPerm
		case strings.HasPrefix(a, "--zone="):
			zone = strings.TrimPrefix(a, "--zone=")
		case strings.HasPrefix(a, "--query-rich-rule="):
			op, rule = "query", strings.TrimPrefix(a, "--query-rich-rule=")
		case strings.HasPrefix(a, "--add-rich-rule="):
			op, rule = "add", strings.TrimPrefix(a, "--add-rich-rule=")
		case strings.HasPrefix(a, "--remove-rich-rule="):
			op, rule = "remove", strings.TrimPrefix(a, "--remove-rich-rule=")
		default:
			return refuse(99, "firewall-cmd stub: unexpected "+a)
		}
	}
	key := zone + "\t" + rule
	switch op {
	case "query":
		if set[key] {
			return ok("yes\n")
		}
		return execx.Result{Stdout: []byte("no\n"), ExitCode: 1}, nil
	case "add":
		if s.fwFailAdd != "" && strings.Contains(rule, s.fwFailAdd) {
			return refuse(1, "Error: COMMAND_FAILED")
		}
		set[key] = true
	case "remove":
		delete(set, key)
	}
	return ok("success\n")
}

// zoneQuery answers the two firewall-cmd questions about zones.
func (s *sim) zoneQuery(arg string) (execx.Result, bool) {
	switch {
	case arg == "--get-default-zone":
		return execx.Result{Stdout: []byte("public\n")}, true
	case strings.HasPrefix(arg, "--get-zone-of-interface="):
		dev := strings.TrimPrefix(arg, "--get-zone-of-interface=")
		if z, found := s.zones[dev]; found {
			return execx.Result{Stdout: []byte(z + "\n")}, true
		}
		return execx.Result{Stdout: []byte("no zone\n"), ExitCode: 2}, true
	}
	return execx.Result{}, false
}

func (s *sim) nmcli(argv []string) (execx.Result, error) {
	switch strings.Join(argv, " ") {
	case "-t -f NAME connection show":
		out := "Wired connection 1\n"
		if s.nmExists {
			out = "agentbus0\n" + out
		}
		return ok(out)
	case "-g ipv4.addresses,ipv6.addresses connection show agentbus0":
		return ok(s.nmAddrs)
	case "-g GENERAL.STATE connection show agentbus0":
		if s.nmUp {
			return ok("activated\n")
		}
		return ok("")
	case "connection up agentbus0":
		s.nmUp = true
		return ok("Connection successfully activated\n")
	}
	if len(argv) > 1 && argv[0] == "connection" && (argv[1] == "add" || argv[1] == "modify") {
		var v4, v6, prev string
		for _, a := range argv {
			if prev == "ipv4.addresses" {
				v4 = a
			}
			if prev == "ipv6.addresses" {
				v6 = a
			}
			prev = a
		}
		s.nmExists, s.nmAddrs = true, v4+"\n"+v6+"\n"
		return ok("Connection 'agentbus0' successfully added.\n")
	}
	return refuse(99, "nmcli stub: unexpected "+strings.Join(argv, " "))
}

func (s *sim) curl(argv []string) (execx.Result, error) {
	var out, url, prev string
	for _, a := range argv {
		if prev == "-o" {
			out = a
		}
		if strings.HasPrefix(a, "http") {
			url = a
		}
		prev = a
	}
	switch {
	case strings.HasPrefix(url, "http://127.0.0.1:") && strings.HasSuffix(url, "/_tuwunel/server_version"):
		if s.anyActive("agent-bus-hs@") || s.anyActive("agent-bus-restore-") {
			return ok(`{"name":"Tuwunel","version":"1.9.3"}`)
		}
		return refuse(7, "curl: (7) Failed to connect")
	case strings.HasPrefix(url, "https://github.com/matrix-construct/tuwunel/releases/download/"):
		if err := os.WriteFile(out, s.asset, 0o600); err != nil {
			return execx.Result{}, err
		}
		return ok("")
	}
	return refuse(99, "curl stub: unexpected url "+url)
}

func (s *sim) ss(argv []string) (execx.Result, error) {
	if !s.anyActive("agent-bus-hs@") {
		return ok("")
	}
	port := argv[len(argv)-1][strings.LastIndex(argv[len(argv)-1], ":")+1:]
	var b strings.Builder
	for _, a := range s.ssAddrs {
		if strings.Contains(a, ":") {
			a = "[" + a + "]"
		}
		fmt.Fprintf(&b, "LISTEN 0 4096 %s:%s 0.0.0.0:*\n", a, port)
	}
	return ok(b.String())
}

func (s *sim) zstd(argv []string) (execx.Result, error) {
	var out string
	for i, a := range argv {
		if a == "-o" {
			out = argv[i+1]
		}
	}
	data, err := os.ReadFile(argv[len(argv)-1])
	if err != nil {
		return execx.Result{}, err
	}
	if err := writeIn(out, data); err != nil {
		return execx.Result{}, err
	}
	return ok("")
}

func (s *sim) bundler(argv []string) (execx.Result, error) {
	app, out := argv[len(argv)-1], ""
	for i, a := range argv {
		if a == "--out" {
			out = argv[i+1]
		}
	}
	if s.bundleBad != "" {
		return ok(s.bundleBad)
	}
	want := []byte("pyz:" + app)
	have, err := readRooted(out)
	if err == nil && bytes.Equal(have, want) {
		return ok("BUNDLE-UNCHANGED " + out + "\n")
	}
	if err := writeIn(out, want); err != nil {
		return execx.Result{}, err
	}
	if err := setMode(out, executableMode); err != nil {
		return execx.Result{}, err
	}
	return ok("BUNDLE-CHANGED " + out + "\n")
}

func (s *sim) adminTool(c execx.Cmd) (execx.Result, error) {
	var stdin []byte
	if c.Stdin != nil {
		var err error
		if stdin, err = io.ReadAll(c.Stdin); err != nil {
			return execx.Result{}, err
		}
	}
	switch c.Args[0] {
	case "render":
		return s.render(c.Args[1:], stdin)
	case "bootstrap":
		return ok(s.bootstrap)
	}
	return refuse(99, "agent-bus stub: unexpected "+c.Args[0])
}

type teamJSON struct {
	Team       string   `json:"team"`
	Port       int      `json:"port"`
	State      string   `json:"state"`
	ServerName string   `json:"server_name"`
	Listen     []string `json:"listen"`
	AllowFrom  []string `json:"allow_from"`
}

func (s *sim) render(args []string, stdin []byte) (execx.Result, error) {
	switch args[0] {
	case "check":
		if s.renderRefuse != 0 {
			return refuse(s.renderRefuse, "team file refused")
		}
		var t teamJSON
		if err := json.Unmarshal(stdin, &t); err != nil {
			return refuse(78, "not JSON")
		}
		if t.Port == 0 {
			t.Port = 8448
		}
		if t.State == "" {
			t.State = "present"
		}
		if t.ServerName == "" {
			t.ServerName = t.Team + ".agent-bus.internal"
		}
		out, err := json.Marshal(t)
		if err != nil {
			return execx.Result{}, err
		}
		return ok(string(out) + "\n")
	case "toml":
		return ok("# tuwunel.toml\n" + string(stdin))
	case "dropin":
		var t teamJSON
		if err := json.Unmarshal(stdin, &t); err != nil {
			return refuse(78, "not JSON")
		}
		return ok("# dropin " + strings.Join(args[1:], " ") + "\nallow " + strings.Join(t.AllowFrom, " ") + "\n")
	case "stamp":
		return ok(`{"config":"c","software":"` + strings.Join(args[1:], " ") + `"}` + "\n")
	}
	return refuse(99, "render stub: unexpected "+args[0])
}

// harness is a prefix, a source tree and an App wired to the simulated host.
type harness struct {
	t      *testing.T
	prefix string
	source string
	sim    *sim
	fake   *execx.Fake
	out    *bytes.Buffer
	errb   *bytes.Buffer
	app    *App
	sleeps int
	digest string
	self   string
	asset  []byte
}

const harnessPID = 4242

func (h *harness) write(path, data string, mode fs.FileMode) {
	h.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		h.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), mode); err != nil {
		h.t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		h.t.Fatal(err)
	}
}

func (h *harness) rel(path string) string { return filepath.Join(h.prefix, path) }

func (h *harness) read(path string) string {
	h.t.Helper()
	data, err := os.ReadFile(h.rel(path))
	if err != nil {
		h.t.Fatal(err)
	}
	return string(data)
}

func (h *harness) mode(path string) fs.FileMode {
	h.t.Helper()
	info, err := os.Stat(h.rel(path))
	if err != nil {
		h.t.Fatal(err)
	}
	return info.Mode().Perm()
}

func (h *harness) exists(path string) bool {
	_, err := os.Lstat(h.rel(path))
	return err == nil
}

func (h *harness) mkdir(path string) {
	h.t.Helper()
	if err := os.MkdirAll(h.rel(path), 0o750); err != nil {
		h.t.Fatal(err)
	}
}

var kitFilesForTest = []string{
	"agent-bus-claude", "settings.json", "plugin/pingbus/.claude-plugin/plugin.json",
	"plugin/pingbus/hooks/hooks.json", "plugin/pingbus/skills/pingbus/SKILL.md",
	"README.podman", "README.host", "README.lxc", "README.docker", "README.vm",
}

const hsUnitText = `# base unit
[Unit]
Description=Agent team bus homeserver for team %i

[Service]
Type=simple
User=agent-bus
Environment=TUWUNEL_CONFIG=/var/lib/agent-bus/%i/tuwunel.toml
ExecStart=/usr/local/lib/agent-bus/tuwunel
ExecStartPre=/bin/true
NoNewPrivileges=yes
ReadWritePaths=/var/lib/agent-bus/%i
IPAddressDeny=any

[Install]
WantedBy=multi-user.target
`

func newHarness(t *testing.T) *harness {
	t.Helper()
	asset := []byte("not really tuwunel, a test fixture\n")
	sum := sha256.Sum256(asset)
	h := &harness{t: t, prefix: t.TempDir(), source: t.TempDir(), out: &bytes.Buffer{}, errb: &bytes.Buffer{}, asset: asset}
	h.digest = strings.Repeat("ab", 32)
	h.sim = newSim(h.prefix, asset)
	h.mkdir("etc/systemd/system")
	h.mkdir("usr/local/bin")
	h.mkdir("usr/local/sbin")
	h.write(h.rel("etc/os-release"), "NAME=\"Fedora Linux\"\nID=fedora\nVERSION_ID=44\n", 0o644)
	h.sim.units["firewalld.service"] = &unitState{active: true}
	h.sim.units["NetworkManager.service"] = &unitState{active: true}
	h.sim.zones["wg0"] = "trusted"

	src := h.source
	h.write(filepath.Join(src, "helpers/pingbus/bundle.py"), "# bundler\n", 0o644)
	h.write(filepath.Join(src, "files/usr/local/bin/agent-bus"), "#!/bin/sh\n# the wrapper\n", 0o755)
	h.write(filepath.Join(src, "files/usr/local/share/agent-bus/resolv.conf"), "nameserver 127.0.0.1\n", 0o644)
	for _, unit := range []string{"agent-bus-hs@.service", "agent-bus-backup@.service", "agent-bus-backup@.timer"} {
		body := "# " + unit + "\n"
		if unit == "agent-bus-hs@.service" {
			body = hsUnitText
		}
		h.write(filepath.Join(src, "files/etc/systemd/system", unit), body, 0o644)
	}
	for _, f := range kitFilesForTest {
		mode := fs.FileMode(0o644)
		if f == "agent-bus-claude" {
			mode = 0o755
		}
		h.write(filepath.Join(src, "files/opt/claude-yolo/optional/agent-bus", f), "kit "+f+"\n", mode)
	}
	h.writePin("1.9.3", hex.EncodeToString(sum[:]))
	h.self = filepath.Join(t.TempDir(), "agent-bus-installer-binary")
	h.write(h.self, "the installer binary\n", 0o755)

	self, err := fsx.CurrentOwner()
	if err != nil {
		t.Fatal(err)
	}
	h.fake = &execx.Fake{}
	h.fake.OnFunc(nil, h.sim.answer)
	h.app = NewApp(Config{
		Prefix:       h.prefix,
		AdminTool:    "agent-bus",
		Euid:         func() int { return 1000 },
		RootOwner:    func() (fsx.Owner, error) { return self, nil },
		SvcOwner:     func() (fsx.Owner, error) { return self, nil },
		Executable:   func() (string, error) { return h.self, nil },
		Runner:       h.fake,
		Stdout:       h.out,
		Stderr:       h.errb,
		Sleep:        func(time.Duration) { h.sleeps++ },
		Rand:         bytes.NewReader(bytes.Repeat([]byte{0xab, 0xcd}, 64)),
		Pid:          harnessPID,
		SourceDigest: h.digest,
	})
	return h
}

func (h *harness) writePin(version, sha string) {
	h.t.Helper()
	pin := "# test pin\ntuwunel_version: v" + version + "\ntuwunel_sha256_x86_64: " + sha +
		"\ntuwunel_sha256_aarch64: " + strings.Repeat("0", 64) + "\n"
	h.write(filepath.Join(h.source, "files/usr/local/share/agent-bus/tuwunel.pin"), pin, 0o644)
}

// run runs the installer with fresh output buffers and a fresh call log.
func (h *harness) run(args ...string) int {
	h.t.Helper()
	h.out.Reset()
	h.errb.Reset()
	h.sim.log = nil
	return h.app.Run(args)
}

func (h *harness) stdout() string { return h.out.String() }
func (h *harness) stderr() string { return h.errb.String() }

// teamFile is a team file in the shape the admin tool's check accepts.
func teamFile(team string, listen, allow []string) string {
	t := teamJSON{Team: team, Listen: listen, AllowFrom: allow}
	out, err := json.Marshal(t)
	if err != nil {
		panic(err)
	}
	return string(out)
}

func (h *harness) teamFile(team string, listen, allow []string) string {
	h.t.Helper()
	path := filepath.Join(h.t.TempDir(), "team.json")
	h.write(path, teamFile(team, listen, allow), 0o644)
	return path
}

func (h *harness) software() {
	h.t.Helper()
	if code := h.run("software", "--source", h.source); code != 0 {
		h.t.Fatalf("software exited %d:\n%s", code, h.stderr())
	}
}

// installTeam runs software and team for team alpha.
func (h *harness) installTeam(allow []string) string {
	h.t.Helper()
	h.software()
	file := h.teamFile("alpha", []string{"192.0.2.10"}, allow)
	if code := h.run("team", "--team-file", file); code != 0 {
		h.t.Fatalf("team exited %d:\n%s", code, h.stderr())
	}
	return file
}

type ownerT = fsx.Owner

// executableMode is the mode of a built zipapp.
const executableMode fs.FileMode = 0o755

func setMode(path string, mode fs.FileMode) error { return os.Chmod(path, mode) }

// writeIn writes a file named by a stub's argv through its directory, so the stub cannot be
// steered out of it by the name.
func writeIn(path string, data []byte) error {
	dir, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return err
	}
	return errors.Join(dir.WriteFile(filepath.Base(path), data, 0o600), dir.Close())
}

func symlink(target, link string) error { return os.Symlink(target, link) }

func removeAll(path string) error { return os.RemoveAll(path) }

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
}

func sha256Hex(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

func contains(t *testing.T, what, text, want string) {
	t.Helper()
	if !strings.Contains(text, want) {
		t.Errorf("%s lacks %q:\n%s", what, want, text)
	}
}

var _ = context.Background
