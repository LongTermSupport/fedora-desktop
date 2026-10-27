package netpolicy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"maps"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// fakeNft is the container namespace's nft as far as the policy uses it: it takes a whole
// table or an allow-set update on `-f -`, and lists the table as JSON.
type fakeNft struct {
	sets    map[string][]string
	rules   int
	applied []string
	extra   string // an element the listing adds to allow4, as a table that drifted would
	refuse  bool
}

var (
	setRE    = regexp.MustCompile(`(?s)set (\w+) \{.*?(?:elements = \{ ([^}]*) \}\n)?    \}`)
	addRE    = regexp.MustCompile(`add element inet ccy_netpolicy (\w+) \{ ([^}]*) \}`)
	flushRE  = regexp.MustCompile(`flush set inet ccy_netpolicy (\w+)`)
	ruleLine = regexp.MustCompile(`(?m)^        (oifname|ip|ip6) .*(accept|admin-prohibited)$`)
)

func (n *fakeNft) apply(text string) error {
	n.applied = append(n.applied, text)
	if n.refuse {
		return errors.New("conflicting intervals specified")
	}
	if strings.HasPrefix(text, "table inet") {
		n.sets = map[string][]string{}
		for _, m := range setRE.FindAllStringSubmatch(text, -1) {
			n.sets[m[1]] = splitElems(m[2])
		}
		n.rules = len(ruleLine.FindAllString(text, -1))
		return nil
	}
	for _, m := range flushRE.FindAllStringSubmatch(text, -1) {
		n.sets[m[1]] = nil
	}
	for _, m := range addRE.FindAllStringSubmatch(text, -1) {
		n.sets[m[1]] = splitElems(m[2])
	}
	return nil
}

// answer is nft's exit for a script: 1 and its message when refused.
func (n *fakeNft) answer(text string) execx.Result {
	if err := n.apply(text); err != nil {
		return execx.Result{ExitCode: 1, Stderr: []byte(err.Error())}
	}
	return execx.Result{}
}

func splitElems(s string) []string {
	var out []string
	for e := range strings.SplitSeq(s, ",") {
		if e = strings.TrimSpace(e); e != "" {
			out = append(out, e)
		}
	}
	return out
}

func (n *fakeNft) list(t *testing.T) []byte {
	t.Helper()
	items := make([]any, 0, 2+len(n.sets)+n.rules)
	items = append(items, map[string]any{"table": map[string]any{"family": "inet", "name": Table}})
	for name, elems := range n.sets {
		if name == "allow4" && n.extra != "" {
			elems = append(slices.Clone(elems), n.extra)
		}
		js := make([]any, 0, len(elems))
		for _, e := range elems {
			p, err := netip.ParsePrefix(e)
			if err != nil {
				js = append(js, e)
				continue
			}
			js = append(js, map[string]any{"prefix": map[string]any{"addr": p.Addr().String(), "len": p.Bits()}})
		}
		items = append(items, map[string]any{"set": map[string]any{"name": name, "table": Table, "elem": js}})
	}
	items = append(items, map[string]any{"chain": map[string]any{"name": "output", "table": Table, "hook": "output", "policy": "accept"}})
	for range n.rules {
		items = append(items, map[string]any{"rule": map[string]any{"table": Table, "chain": "output"}})
	}
	out, err := json.Marshal(map[string]any{"nftables": items})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

type world struct {
	t       *testing.T
	fake    *execx.Fake
	nft     *fakeNft
	env     map[string]string
	home    string
	runtime string
	dir     string
	stdout  bytes.Buffer
	stderr  bytes.Buffer
	ifaces  []Iface
	names   map[string][]netip.Addr
	noNft   bool
	inspect map[string]any
	exit    int
	// started is the state directory's files as they were while the session ran.
	started map[string][]byte
}

const (
	name  = "repo_yolo"
	image = "claude-yolo:latest"
)

func newWorld(t *testing.T) *world {
	t.Helper()
	w := &world{
		t: t, fake: &execx.Fake{}, nft: &fakeNft{}, home: t.TempDir(), runtime: t.TempDir(), dir: filepath.Join(t.TempDir(), "repo"),
		env:   map[string]string{},
		names: map[string][]netip.Addr{"build.example.com": {netip.MustParseAddr("198.51.100.5")}},
		ifaces: []Iface{
			{"lo", []netip.Prefix{mustPrefix(t, "127.0.0.1/8")}},
			{"eth0", []netip.Prefix{v4(192, 168, 1, 7, 24), mustPrefix(t, "2001:db8::7/64")}},
			{BusIface, []netip.Prefix{v4(10, 254, 0, 1, 32)}},
			{LXCBridge, []netip.Prefix{v4(10, 0, 3, 1, 24)}},
		},
	}
	resolv := filepath.Join(t.TempDir(), "resolv.conf")
	w.write(resolv, "nameserver "+addr4(169, 254, 1, 1).String()+"\nnameserver 192.0.2.53\n")
	w.inspect = map[string]any{
		"State":           map[string]any{"Pid": 4242, "Running": true},
		"ResolvConfPath":  resolv,
		"HostConfig":      map[string]any{"Privileged": false},
		"EffectiveCaps":   []string{"CAP_CHOWN", "CAP_KILL", "CAP_NET_BIND_SERVICE"},
		"NetworkSettings": map[string]any{"Networks": map[string]any{"podman": map[string]any{}}},
	}
	w.script()
	return w
}

func (w *world) script() {
	t := w.t
	w.scriptEngine()
	w.fake.On([]string{"podman", "create"}, execx.Result{Stdout: []byte("0123abcd\n")})
	w.fake.On([]string{"podman", "init"}, execx.Result{})
	w.fake.On([]string{"podman", "rm"}, execx.Result{})
	w.fake.OnFunc([]string{"podman", "inspect"}, func(execx.Cmd) (execx.Result, error) {
		out, err := json.Marshal([]any{w.inspect})
		return execx.Result{Stdout: out}, err
	})
	w.fake.OnFunc([]string{"podman", "network", "inspect"}, func(c execx.Cmd) (execx.Result, error) {
		net := c.Args[len(c.Args)-1]
		subnet := map[string]string{"shop-network": v4(10, 89, 6, 0, 24).String(), "other-network": v4(10, 89, 7, 0, 24).String()}[net]
		if subnet == "" {
			return execx.Result{ExitCode: 125, Stderr: []byte("network not found")}, nil
		}
		out, err := json.Marshal([]any{map[string]any{"name": net, "subnets": []any{map[string]string{"subnet": subnet}}}})
		return execx.Result{Stdout: out}, err
	})
	w.fake.OnFunc([]string{"podman", "unshare", "nsenter", "-t", "4242", "-n", "nft", "-f", "-"}, func(c execx.Cmd) (execx.Result, error) {
		text, err := io.ReadAll(c.Stdin)
		if err != nil {
			return execx.Result{}, err
		}
		return w.nft.answer(string(text)), nil
	})
	w.fake.OnFunc([]string{"podman", "unshare", "nsenter", "-t", "4242", "-n", "nft", "-j", "list", "table", "inet", Table}, func(execx.Cmd) (execx.Result, error) {
		return execx.Result{Stdout: w.nft.list(t)}, nil
	})
}

func (w *world) scriptEngine() {
	// The engine's every other command, first: a later, longer rule wins.
	for _, engine := range []string{"podman", "docker"} {
		w.fake.OnFunc([]string{engine}, func(c execx.Cmd) (execx.Result, error) {
			if c.Args[0] == "start" || c.Args[0] == "run" {
				w.started = map[string][]byte{}
				entries, err := os.ReadDir(w.stateDir())
				if err != nil {
					return execx.Result{}, err
				}
				for _, e := range entries {
					data, err := os.ReadFile(filepath.Join(w.stateDir(), e.Name()))
					if err != nil {
						return execx.Result{}, err
					}
					w.started[e.Name()] = data
				}
			}
			return execx.Result{ExitCode: w.exit}, nil
		})
	}
}

func (w *world) write(path, text string) {
	w.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		w.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		w.t.Fatal(err)
	}
}

func (w *world) stateDir() string { return filepath.Join(w.runtime, "ccy", "network-policy", name) }

func (w *world) app(args ...string) App {
	env := map[string]string{"HOME": w.home, "XDG_RUNTIME_DIR": w.runtime, "CCY_MOUNT_RELABEL": "z"}
	maps.Copy(env, w.env)
	return App{
		Args:   args,
		Getenv: func(k string) string { return env[k] },
		Runner: w.fake, Fg: w.fake,
		LookPath: func(p string) (string, error) {
			if p == "nft" && w.noNft {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + p, nil
		},
		Ifaces: func() ([]Iface, error) { return w.ifaces, nil },
		Resolve: func(_ context.Context, host string) ([]netip.Addr, error) {
			if a, ok := w.names[host]; ok {
				return a, nil
			}
			return nil, errors.New("no such host")
		},
		HostsFile: filepath.Join(w.home, "hosts"),
		Dir:       w.dir,
		Stdout:    &w.stdout, Stderr: &w.stderr,
	}
}

// launchArgs is what the bash launcher hands over: the engine, then its run argv.
func launchArgs(engine string, opts []string, cmd ...string) []string {
	args := []string{"launch", "--name", name, "--image", image, "--", engine, "run", "-it", "--rm", "--name", name}
	args = append(args, opts...)
	return append(append(args, image), cmd...)
}

func (w *world) run(want term.ExitCode, args ...string) {
	w.t.Helper()
	if got := w.app(args...).Run(context.Background()); got != want {
		w.t.Fatalf("exit %d, want %d; stderr:\n%s", got, want, w.stderr.String())
	}
}

func (w *world) lines() []string {
	out := make([]string, 0, len(w.fake.Calls())+len(w.fake.Foregrounds()))
	for _, c := range w.fake.Calls() {
		out = append(out, c.Line())
	}
	for _, c := range w.fake.Foregrounds() {
		out = append(out, "FG "+c.Line())
	}
	return out
}

func (w *world) call(prefix string) execx.Cmd {
	w.t.Helper()
	cmds := w.fake.Calls()
	if rest, ok := strings.CutPrefix(prefix, "FG "); ok {
		cmds, prefix = w.fake.Foregrounds(), rest
	}
	for _, c := range cmds {
		if strings.HasPrefix(c.Line(), prefix) {
			return c
		}
	}
	w.t.Fatalf("no call %q among %v", prefix, w.lines())
	return execx.Cmd{}
}

func promptOf(c execx.Cmd) string {
	i := slices.Index(c.Args, "--append-system-prompt")
	if i < 0 || i+1 >= len(c.Args) {
		return ""
	}
	return c.Args[i+1]
}

func TestLaunchRunsInOrder(t *testing.T) {
	w := newWorld(t)
	w.exit = 75
	w.run(75, launchArgs("podman", []string{"--network", "podman", "-e", "GH_TOKEN"}, "claude", "--dangerously-skip-permissions", "-p", "hi")...)
	order := []string{"podman create", "podman init", "podman inspect", "podman unshare nsenter -t 4242 -n nft -f -", "podman unshare nsenter -t 4242 -n nft -j list", "FG podman start --attach --interactive " + name}
	lines := w.lines()
	last := -1
	for _, want := range order {
		i := slices.IndexFunc(lines, func(l string) bool { return strings.HasPrefix(l, want) })
		if i <= last {
			t.Fatalf("%q is not in order in %v", want, lines)
		}
		last = i
	}
}

func TestLaunchEnforcesTheDefault(t *testing.T) {
	w := newWorld(t)
	w.run(0, launchArgs("podman", []string{"--network", "podman", "-e", "GH_TOKEN"}, "claude", "--dangerously-skip-permissions", "-p", "hi")...)
	create := w.call("podman create")
	argv := create.Line()
	if !strings.Contains(argv, "-v "+filepath.Join(w.stateDir(), "context")+":"+ContextPath+":ro,z ") {
		t.Errorf("no read-only context mount: %s", argv)
	}
	if !strings.Contains(argv, "-it --rm --name "+name+" --network podman -e GH_TOKEN") {
		t.Errorf("the run options were not kept: %s", argv)
	}
	prompt := promptOf(create)
	if !strings.HasPrefix(prompt, Marker) || !strings.Contains(prompt, "agent bus at "+addr4(10, 254, 0, 1).String()) {
		t.Errorf("the system prompt: %q", prompt)
	}
	if i := slices.Index(create.Args, image); i < 0 || create.Args[i+1] != "claude" || create.Args[i+2] != "--append-system-prompt" {
		t.Errorf("the prompt is not the agent's: %v", create.Args)
	}
	if w.started["context"] == nil || !strings.HasPrefix(string(w.started["context"]), Marker) || w.started["state.json"] == nil {
		t.Errorf("the session ran without its context file and state: %v", w.started)
	}
	if _, err := os.Stat(w.stateDir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the state outlived the session: %v", err)
	}
	if !strings.Contains(w.stderr.String(), "Network policy held") {
		t.Errorf("stderr: %s", w.stderr.String())
	}
}

func TestLaunchOpensProjectNetworksAndTheOwnersList(t *testing.T) {
	w := newWorld(t)
	w.write(HostFile(w.home, w.dir), "# build cache\nCCY_NETWORK_ALLOW=build.example.com:8443,lxc\n")
	w.write(filepath.Join(w.home, "hosts"), addr4(10, 0, 3, 16).String()+" site-a\n")
	w.env[AllowVar] = "203.0.113.7"
	w.run(0, launchArgs("podman", []string{"--network", "shop-network"}, "claude")...)
	table := w.nft.applied[0]
	for _, want := range []string{v4(10, 89, 6, 0, 24).String(), "198.51.100.5 tcp dport 8443 accept", addr4(10, 0, 3, 16).String(), "203.0.113.7"} {
		if !strings.Contains(table, want) {
			t.Errorf("the table lacks %s:\n%s", want, table)
		}
	}
	prompt := promptOf(w.call("podman create"))
	for _, want := range []string{"shop-network (" + v4(10, 89, 6, 0, 24).String() + ")", "build.example.com:8443, lxc (1 LXC dev-site addresses), 203.0.113.7", HostFile(w.home, w.dir)} {
		if !strings.Contains(prompt, want) {
			t.Errorf("the prompt lacks %q:\n%s", want, prompt)
		}
	}
}

func TestLaunchTableHoldsTheDerivedAllows(t *testing.T) {
	w := newWorld(t)
	w.run(0, launchArgs("podman", []string{"--network", "podman"}, "claude")...)
	table := w.nft.applied[0]
	for _, want := range []string{addr4(10, 254, 0, 1).String(), addr4(169, 254, 1, 1).String(), "192.0.2.53", "2001:db8::7", "admin-prohibited"} {
		if !strings.Contains(table, want) {
			t.Errorf("the table lacks %s:\n%s", want, table)
		}
	}
	if strings.Contains(table, v4(10, 88, 0, 0, 16).String()) {
		t.Errorf("the shared bridge was opened:\n%s", table)
	}
}

func TestLaunchMergesAnExistingSystemPrompt(t *testing.T) {
	w := newWorld(t)
	w.run(0, launchArgs("podman", nil, "claude", "--dangerously-skip-permissions", "--append-system-prompt", "STARTUP")...)
	create := w.call("podman create")
	if prompt := promptOf(create); !strings.HasPrefix(prompt, "STARTUP\n\n"+Marker) {
		t.Errorf("prompt %q", prompt)
	}
	if strings.Count(create.Line(), "--append-system-prompt") != 1 {
		t.Errorf("two system prompts: %v", create.Args)
	}
}

func TestLaunchOtherAgentGetsTheFileOnly(t *testing.T) {
	w := newWorld(t)
	w.run(0, launchArgs("podman", nil, "opencode", "--auto")...)
	create := w.call("podman create")
	if promptOf(create) != "" || !strings.HasSuffix(create.Line(), image+" opencode --auto") {
		t.Errorf("opencode's command was changed: %v", create.Args)
	}
}

func TestLaunchOff(t *testing.T) {
	w := newWorld(t)
	w.write(HostFile(w.home, w.dir), "CCY_NETWORK_ALLOW=off\n")
	w.exit = 3
	w.run(3, launchArgs("podman", nil, "claude")...)
	run := w.call("FG podman run")
	if !strings.Contains(promptOf(run), "off for this checkout") || w.fake.Called("podman create") {
		t.Errorf("off: %v", w.lines())
	}
	if !strings.Contains(w.stderr.String(), "off") {
		t.Errorf("stderr: %s", w.stderr.String())
	}
}

func TestLaunchUnavailableWarnsAndRuns(t *testing.T) {
	for _, c := range []struct {
		name, engine, warn string
		noNft              bool
	}{
		{"docker", "docker", "Docker", false},
		{"no nft", "podman", "play-claude-yolo.yml", true},
	} {
		t.Run(c.name, func(t *testing.T) {
			w := newWorld(t)
			w.noNft = c.noNft
			w.run(0, launchArgs(c.engine, nil, "claude")...)
			run := w.call("FG " + c.engine + " run")
			if !strings.Contains(promptOf(run), "NO network policy") {
				t.Errorf("prompt %q", promptOf(run))
			}
			if !strings.Contains(w.stderr.String(), "WARNING") || !strings.Contains(w.stderr.String(), c.warn) {
				t.Errorf("stderr: %s", w.stderr.String())
			}
		})
	}
}

func TestLaunchFailsClosed(t *testing.T) {
	cases := map[string]struct {
		set  func(w *world)
		want string
	}{
		"privileged":       {func(w *world) { w.inspect["HostConfig"] = map[string]any{"Privileged": true} }, "privileged"},
		"NET_ADMIN":        {func(w *world) { w.inspect["EffectiveCaps"] = []string{"CAP_NET_ADMIN"} }, "CAP_NET_ADMIN"},
		"SYS_ADMIN":        {func(w *world) { w.inspect["EffectiveCaps"] = []string{"CAP_SYS_ADMIN"} }, "CAP_SYS_ADMIN"},
		"apply refused":    {func(w *world) { w.nft.refuse = true }, "conflicting intervals"},
		"read back drifts": {func(w *world) { w.nft.extra = "198.51.100.0/24" }, "allow4"},
		"no pid":           {func(w *world) { w.inspect["State"] = map[string]any{"Pid": 0} }, "pid"},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			w := newWorld(t)
			c.set(w)
			w.run(term.ExitFailure, launchArgs("podman", nil, "claude")...)
			if len(w.fake.Foregrounds()) != 0 {
				t.Errorf("the session was started: %v", w.lines())
			}
			if !w.fake.Called("podman rm -f -t 0 " + name) {
				t.Errorf("the container was not removed: %v", w.lines())
			}
			if !strings.Contains(w.stderr.String(), c.want) || !strings.Contains(w.stderr.String(), "not started") {
				t.Errorf("stderr: %s", w.stderr.String())
			}
		})
	}
}

func TestLaunchRefusesBeforeCreate(t *testing.T) {
	cases := map[string]struct {
		set  func(w *world)
		opts []string
		want string
	}{
		"unresolved name":  {func(w *world) { w.env[AllowVar] = "nowhere.example.com" }, nil, "nowhere.example.com"},
		"bad host file":    {func(w *world) { w.write(HostFile(w.home, w.dir), "rm -rf /\n") }, nil, "line 1"},
		"bad setting":      {func(w *world) { w.env[AllowVar] = "192.0.2.1:x" }, nil, AllowVar},
		"no runtime dir":   {func(w *world) { w.runtime = "" }, nil, "XDG_RUNTIME_DIR"},
		"host network":     {func(*world) {}, []string{"--network=host"}, "host"},
		"unknown network":  {func(*world) {}, []string{"--network", "gone-network"}, "gone-network"},
		"lxc but no sites": {func(w *world) { w.env[AllowVar] = "lxc" }, nil, "lxc"},
	}
	for label, c := range cases {
		t.Run(label, func(t *testing.T) {
			w := newWorld(t)
			c.set(w)
			w.run(term.ExitFailure, launchArgs("podman", c.opts, "claude")...)
			if w.fake.Called("podman create") || len(w.fake.Foregrounds()) != 0 {
				t.Errorf("a container was made: %v", w.lines())
			}
			if !strings.Contains(w.stderr.String(), c.want) {
				t.Errorf("stderr does not name %q: %s", c.want, w.stderr.String())
			}
		})
	}
}

func TestUsage(t *testing.T) {
	for _, args := range [][]string{
		nil,
		{"nonsense"},
		{"launch"},
		{"launch", "--name", name, "--image", image, "--", "podman", "exec", image},
		{"launch", "--name", name, "--image", image, "--", "podman", "run", "-it"},
		{"launch", "--name", name, "--", "podman", "run", image},
		{"refresh", "extra"},
		{"where", "x"},
		{"launch", "--bogus", "x", "--", "podman", "run", image},
	} {
		w := newWorld(t)
		w.run(term.ExitUsage, args...)
		if !strings.Contains(w.stderr.String(), "usage") {
			t.Errorf("%v: stderr %s", args, w.stderr.String())
		}
	}
}

func TestWhere(t *testing.T) {
	w := newWorld(t)
	w.run(0, "where")
	if got := w.stdout.String(); got != HostFile(w.home, w.dir)+"\n" {
		t.Errorf("stdout %q", got)
	}
}

// launched is a world whose session was launched on the bridge and is still running: its
// state directory put back as it was while the session ran.
func launched(t *testing.T) *world {
	t.Helper()
	w := newWorld(t)
	w.run(0, launchArgs("podman", []string{"--network", "podman"}, "claude")...)
	for file, data := range w.started {
		w.write(filepath.Join(w.stateDir(), file), string(data))
	}
	return w
}

func TestRefreshOpensAJoinedNetwork(t *testing.T) {
	w := launched(t)
	w.inspect["NetworkSettings"] = map[string]any{"Networks": map[string]any{"podman": map[string]any{}, "other-network": map[string]any{}}}
	w.run(0, "refresh")
	update := w.nft.applied[len(w.nft.applied)-1]
	if !strings.HasPrefix(update, "flush set inet ccy_netpolicy allow4\nadd element") || !strings.Contains(update, v4(10, 89, 7, 0, 24).String()) {
		t.Errorf("update %q", update)
	}
	context, err := os.ReadFile(filepath.Join(w.stateDir(), "context"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(context), "other-network") {
		t.Errorf("the context file was not rewritten:\n%s", context)
	}
	w.inspect["NetworkSettings"] = map[string]any{"Networks": map[string]any{"podman": map[string]any{}}}
	w.run(0, "refresh")
	if update := w.nft.applied[len(w.nft.applied)-1]; strings.Contains(update, v4(10, 89, 7, 0, 24).String()) {
		t.Errorf("a left network is still open: %q", update)
	}
}

func TestRefreshSkipsAndCleans(t *testing.T) {
	w := launched(t)
	w.inspect["State"] = map[string]any{"Pid": 4242, "Running": false}
	w.run(0, "refresh")
	if strings.HasPrefix(w.nft.applied[len(w.nft.applied)-1], "flush") {
		t.Error("a stopped session was refreshed")
	}
	w.fake.On([]string{"podman", "inspect"}, execx.Result{ExitCode: 125, Stderr: []byte("no such container")})
	w.run(0, "refresh")
	if _, err := os.Stat(w.stateDir()); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("a gone session's state was kept: %v", err)
	}
	w.run(0, "refresh")
}

func TestRefreshFailureIsLoud(t *testing.T) {
	w := launched(t)
	w.nft.extra = "198.51.100.0/24"
	w.run(term.ExitFailure, "refresh")
	if !strings.Contains(w.stderr.String(), name) {
		t.Errorf("stderr: %s", w.stderr.String())
	}
}
