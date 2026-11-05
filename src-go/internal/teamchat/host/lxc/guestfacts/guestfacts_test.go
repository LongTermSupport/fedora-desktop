package guestfacts

import (
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const guestAccounts = `root:x:0:0:root:/root:/bin/bash
# a comment

daemon:x:2:2:daemon:/sbin:/usr/sbin/nologin
agent:x:1001:1002:The Agent:/srv/agent:/bin/bash
other:x:1003:1003::/srv/other:/bin/sh
`

func agentUser(t *testing.T) paths.GuestUser {
	t.Helper()
	u, err := paths.ParseGuestUser("agent")
	if err != nil {
		t.Fatal(err)
	}
	return u
}

func refused(t *testing.T, what string, err error, want string) {
	t.Helper()
	if !errors.Is(err, ErrRefused) {
		t.Errorf("%s: %v, want ErrRefused", what, err)
		return
	}
	if !strings.Contains(err.Error(), want) {
		t.Errorf("%s: %q does not say %q", what, err, want)
	}
}

func TestPasswdFindsTheUser(t *testing.T) {
	acct, err := Passwd([]byte(guestAccounts), agentUser(t), map[int]bool{0: true, 1000: true})
	if err != nil {
		t.Fatal(err)
	}
	if acct != (Account{User: agentUser(t), UID: 1001, GID: 1002, Home: "/srv/agent"}) {
		t.Fatalf("account %+v", acct)
	}
	// No host uid set is no K15 check (the L3 probe's read).
	if _, err := Passwd([]byte(guestAccounts), agentUser(t), nil); err != nil {
		t.Fatalf("nil host uids: %v", err)
	}
}

func TestPasswdRefusals(t *testing.T) {
	agent := agentUser(t)
	line := func(l string) []byte { return []byte("root:x:0:0::/root:/bin/sh\n" + l + "\n") }
	for name, c := range map[string]struct {
		data []byte
		host map[int]bool
		want string
	}{
		"missing":          {[]byte("root:x:0:0::/root:/bin/sh\n"), nil, "no account"},
		"host uid":         {[]byte(guestAccounts), map[int]bool{1001: true}, "K15"},
		"uid 0":            {line("agent:x:0:1002::/srv/agent:/bin/sh"), nil, "uid"},
		"gid 0":            {line("agent:x:1001:0::/srv/agent:/bin/sh"), nil, "gid"},
		"uid sign":         {line("agent:x:+1001:1002::/srv/agent:/bin/sh"), nil, "uid"},
		"uid word":         {line("agent:x:abc:1002::/srv/agent:/bin/sh"), nil, "uid"},
		"uid huge":         {line("agent:x:4294967295:1002::/srv/agent:/bin/sh"), nil, "uid"},
		"gid empty":        {line("agent:x:1001:::/srv/agent:/bin/sh"), nil, "gid"},
		"home relative":    {line("agent:x:1001:1002::home/agent:/bin/sh"), nil, "home"},
		"home root":        {line("agent:x:1001:1002::/:/bin/sh"), nil, "home"},
		"home dotdot":      {line("agent:x:1001:1002::/srv/../agent:/bin/sh"), nil, "home"},
		"home trailing":    {line("agent:x:1001:1002::/srv/agent/:/bin/sh"), nil, "home"},
		"home space":       {line("agent:x:1001:1002::/srv/a gent:/bin/sh"), nil, "home"},
		"home specifier":   {line("agent:x:1001:1002::/srv/%h:/bin/sh"), nil, "home"},
		"home dollar":      {line("agent:x:1001:1002::/srv/$x:/bin/sh"), nil, "home"},
		"twice":            {line("agent:x:1001:1002::/srv/agent:/bin/sh\nagent:x:1004:1004::/srv/b:/bin/sh"), nil, "twice"},
		"uid shared":       {line("other:x:1001:1003::/srv/o:/bin/sh\nagent:x:1001:1002::/srv/agent:/bin/sh"), nil, "shares"},
		"six fields":       {line("agent:x:1001:1002::/srv/agent"), nil, "line"},
		"eight fields":     {line("agent:x:1001:1002::/srv/agent:/bin/sh:x"), nil, "line"},
		"other line bad":   {[]byte("broken\n" + guestAccounts), nil, "line"},
		"not utf-8":        {line("agent:x:1001:1002:\xff:/srv/agent:/bin/sh"), nil, "UTF-8"},
		"nul":              {line("agent:x:1001:1002:\x00:/srv/agent:/bin/sh"), nil, "control"},
		"carriage return":  {line("agent:x:1001:1002::/srv/agent:/bin/sh\r"), nil, "control"},
		"over the cap":     {append([]byte(guestAccounts), make([]byte, MaxPasswd)...), nil, "bytes"},
		"empty file":       {nil, nil, "no account"},
		"name differs":     {line("Agent:x:1001:1002::/srv/agent:/bin/sh"), nil, "no account"},
		"prefix not name":  {line("agentx:x:1001:1002::/srv/agent:/bin/sh"), nil, "no account"},
		"uid leading zero": {line("agent:x:01001:1002::/srv/agent:/bin/sh"), nil, "uid"},
	} {
		_, err := Passwd(c.data, agent, c.host)
		refused(t, name, err, c.want)
	}
}

// No guest byte reaches an error: a refusal names the rule, never the value.
func TestRefusalsDoNotEchoGuestBytes(t *testing.T) {
	_, err := Passwd([]byte("agent:x:1001:1002::/srv/SECRET value:/bin/sh\n"), agentUser(t), nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error %v", err)
	}
	_, err = Prefix(member(t, ""), []byte("[remote \"origin\"]\n\turl = SECRET\"\n"), nil)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("error %v", err)
	}
}

func TestHostUIDs(t *testing.T) {
	got, err := HostUIDs([]byte(guestAccounts))
	if err != nil {
		t.Fatal(err)
	}
	want := map[int]bool{0: true, 2: true, 1001: true, 1003: true}
	if len(got) != len(want) {
		t.Fatalf("uids %v", got)
	}
	for uid := range want {
		if !got[uid] {
			t.Errorf("uid %d missing from %v", uid, got)
		}
	}
	for name, data := range map[string]string{
		"bad uid":  "a:x:z:0::/:/bin/sh\n",
		"bad line": "a:x:0\n",
		"empty":    "",
		"comments": "# nothing\n\n",
	} {
		_, err := HostUIDs([]byte(data))
		refused(t, name, err, "host passwd")
	}
	if _, err := HostUIDs(make([]byte, MaxPasswd+1)); !errors.Is(err, ErrRefused) {
		t.Errorf("over the cap: %v", err)
	}
}

func TestGroupName(t *testing.T) {
	group := "root:x:0:\nagents:x:1002:agent,other\nalias:x:1002:\n"
	name, err := GroupName([]byte(group), 1002)
	if err != nil || name != "agents" {
		t.Fatalf("GroupName = %q, %v", name, err)
	}
	for what, c := range map[string]struct {
		data string
		want string
	}{
		"missing":      {group, "no group"},
		"bad name":     {"Agents:x:1003:\n", "name"},
		"shadowed":     {"agents:x:7:\nagents:x:1003:\n", "name"},
		"bad line":     {"agents:x:1003\n", "line"},
		"bad gid":      {"agents:x:x:\n", "gid"},
		"over the cap": {strings.Repeat("#", MaxPasswd+1), "bytes"},
	} {
		_, err := GroupName([]byte(c.data), 1003)
		refused(t, what, err, c.want)
	}
}

func member(t *testing.T, site string) declare.LXCMember {
	t.Helper()
	team, err := paths.ParseTeamID("ops")
	if err != nil {
		t.Fatal(err)
	}
	return declare.LXCMember{Team: team, Container: "box-1", User: agentUser(t), Checkout: "/srv/My.Thing", Site: site}
}

func gitConfig(url string) []byte {
	return []byte("[core]\n\trepositoryformatversion = 0\n\tbare = false\n\teditor = \"code --wait\"\n" +
		"[remote \"origin\"]\n\turl = " + url + "\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n" +
		"[branch \"main\"]\n\tremote = origin\n")
}

func TestPrefixRepoFromOrigin(t *testing.T) {
	for url, repo := range map[string]string{
		"https://github.com/owner/Fedora-Desktop.git": "fedora-desktop",
		"git@github.com:owner/thing.git":              "thing",
		"git@github.com:thing.git":                    "thing",
		"ssh://git@example.com/a/b/repo/":             "repo",
		"https://example.com/a/repo.git/":             "repo",
		"/srv/git/local.git":                          "local",
		"https://example.com/a/My_Repo # a comment":   "my_repo",
	} {
		p, err := Prefix(member(t, ""), gitConfig(url), nil)
		if err != nil {
			t.Errorf("%s: %v", url, err)
			continue
		}
		if p != (proto.Prefix{Repo: repo, Site: DefaultSite, Type: "lxc"}) {
			t.Errorf("%s: prefix %+v, want repo %s", url, p, repo)
		}
	}
}

// With no origin, the repo is the checkout directory's base name normalised (v1's rule).
func TestPrefixRepoFromTheCheckoutWithNoOrigin(t *testing.T) {
	p, err := Prefix(member(t, ""), []byte("[core]\n\tbare = false\n[remote \"upstream\"]\n\turl = https://example.com/x/y.git\n"), nil)
	if err != nil || p.String() != "my-thing+local.lxc" {
		t.Fatalf("prefix %v, %v", p, err)
	}
}

func TestPrefixSite(t *testing.T) {
	cfg := gitConfig("https://example.com/o/repo.git")
	for name, c := range map[string]struct {
		site string
		env  []byte
		want string
	}{
		"entry wins":    {"desk", []byte("HOOKS_DAEMON_HOSTNAME=other\n"), "repo+desk.lxc"},
		"env":           {"", []byte("# set by ccy\n\nexport FOO=bar\nHOOKS_DAEMON_HOSTNAME=Box_One\n"), "repo+box-one.lxc"},
		"no file":       {"", nil, "repo+local.lxc"},
		"no assignment": {"", []byte("FOO=1\n"), "repo+local.lxc"},
		"indented note": {"", []byte("  # note\n\t\nexport HOOKS_DAEMON_HOSTNAME=h1\n"), "repo+h1.lxc"},
	} {
		p, err := Prefix(member(t, c.site), cfg, c.env)
		if err != nil || p.String() != c.want {
			t.Errorf("%s: prefix %v, %v; want %s", name, p, err, c.want)
		}
	}
}

func TestPrefixRefusesTheEnvFile(t *testing.T) {
	cfg := gitConfig("https://example.com/o/repo.git")
	for name, env := range map[string]string{
		"dollar":        "HOOKS_DAEMON_HOSTNAME=$(id)\n",
		"backtick":      "HOOKS_DAEMON_HOSTNAME=`id`\n",
		"double quote":  "HOOKS_DAEMON_HOSTNAME=\"h\"\n",
		"single quote":  "HOOKS_DAEMON_HOSTNAME='h'\n",
		"space":         "HOOKS_DAEMON_HOSTNAME=a b\n",
		"backslash":     "HOOKS_DAEMON_HOSTNAME=a\\\n",
		"semicolon":     "HOOKS_DAEMON_HOSTNAME=a;id\n",
		"second":        "HOOKS_DAEMON_HOSTNAME=a\nHOOKS_DAEMON_HOSTNAME=b\n",
		"second other":  "FOO=a\nexport FOO=b\n",
		"command":       "source other.env\n",
		"bad name":      "1FOO=a\n",
		"export alone":  "export FOO\n",
		"no site":       "HOOKS_DAEMON_HOSTNAME=\n",
		"site grammar":  "HOOKS_DAEMON_HOSTNAME=---\n",
		"over the cap":  strings.Repeat("#", MaxCheckout+1),
		"control":       "FOO=a\x07\n",
		"trailing tail": "FOO=a # c\n",
	} {
		_, err := Prefix(member(t, ""), cfg, []byte(env))
		refused(t, name, err, "ccy.env.local")
	}
}

func TestPrefixRefusesTheGitConfig(t *testing.T) {
	for name, cfg := range map[string]string{
		"insteadOf":                "[url \"https://evil.example/\"]\n\tinsteadOf = https://github.com/\n" + string(gitConfig("https://github.com/o/r.git")),
		"pushInsteadOf any case":   string(gitConfig("https://github.com/o/r.git")) + "[url \"x\"]\n\tPUSHINSTEADOF = y\n",
		"insteadOf in origin":      "[remote \"origin\"]\n\turl = https://github.com/o/r.git\n\tinsteadof = z\n",
		"include":                  "[include]\n\tpath = other\n" + string(gitConfig("https://github.com/o/r.git")),
		"includeIf":                "[includeIf \"gitdir:/x/\"]\n\tpath = other\n" + string(gitConfig("https://github.com/o/r.git")),
		"two urls":                 "[remote \"origin\"]\n\turl = https://a.example/o/r.git\n\turl = https://b.example/o/s.git\n",
		"two sections two urls":    "[remote \"origin\"]\n\turl = https://a.example/o/r.git\n[remote \"origin\"]\n\turl = https://b.example/o/s.git\n",
		"origin with no url":       "[remote \"origin\"]\n\tfetch = +refs/heads/*:refs/remotes/origin/*\n",
		"quoted url":               "[remote \"origin\"]\n\turl = \"https://a.example/o/r.git\"\n",
		"continuation":             "[core]\n\teditor = vi \\\n[remote \"origin\"]\n\turl = https://a.example/o/r.git\n",
		"key before section":       "url = https://a.example/o/r.git\n",
		"key on header":            "[remote \"origin\"] url = https://a.example/o/r.git\n",
		"bad key":                  "[core]\n\t-x = 1\n",
		"bad header":               "[remote \"origin\"\n",
		"dotted header":            "[remote.origin]\n\turl = https://b.example/o/s.git\n" + string(gitConfig("https://github.com/o/r.git")),
		"url with no component":    "[remote \"origin\"]\n\turl = host:\n",
		"url normalises to none":   "[remote \"origin\"]\n\turl = https://a.example/o/---.git\n",
		"empty url":                "[remote \"origin\"]\n\turl =\n",
		"an escaped second origin": "[remote \"origin\"]\n\turl = https://a.example/o/r.git\n[remote \"orig\\in\"]\n\turl = https://b.example/o/s.git\n",
		"not utf-8":                "[core]\n\tx = \xff\n",
		"control":                  "[core]\n\tx = \x1b[2J\n",
		"over the cap":             strings.Repeat("#", MaxCheckout+1),
	} {
		_, err := Prefix(member(t, ""), []byte(cfg), nil)
		refused(t, name, err, ".git/config")
	}
	if _, err := Prefix(member(t, ""), nil, nil); !errors.Is(err, ErrRefused) {
		t.Errorf("no git config: %v", err)
	}
}

// The base-name fallback is refused when nothing is left of it.
func TestPrefixRefusesAnUnusableCheckoutName(t *testing.T) {
	m := member(t, "")
	m.Checkout = "/srv/---"
	_, err := Prefix(m, []byte("[core]\n\tbare = false\n"), nil)
	refused(t, "checkout", err, "checkout")
}

// A subsection is case sensitive, so ORIGIN is not origin: the base name is used.
func TestPrefixOriginIsCaseSensitive(t *testing.T) {
	p, err := Prefix(member(t, ""), []byte("[REMOTE \"ORIGIN\"]\n\turl = https://a.example/o/r.git\n"), nil)
	if err != nil || p.Repo != "my-thing" {
		t.Fatalf("prefix %v, %v", p, err)
	}
	p, err = Prefix(member(t, ""), []byte("[Remote \"origin\"]\n\tURL = https://a.example/o/r.git\n"), nil)
	if err != nil || p.Repo != "r" {
		t.Fatalf("section and key case: prefix %v, %v", p, err)
	}
}

// B6: git reads a subsection's backslash as an escape of the next byte, so orig\in is origin.
func TestPrefixOriginSubsectionEscapes(t *testing.T) {
	for _, header := range []string{`[remote "orig\in"]`, `[remote "\o\r\i\g\i\n"]`} {
		p, err := Prefix(member(t, ""), []byte(header+"\n\turl = https://a.example/o/r.git\n"), nil)
		if err != nil || p.Repo != "r" {
			t.Errorf("%s: prefix %v, %v", header, p, err)
		}
	}
	p, err := Prefix(member(t, ""), []byte(`[remote "origin\"x"]`+"\n\turl = https://a.example/o/r.git\n"), nil)
	if err != nil || p.Repo != "my-thing" {
		t.Errorf("an escaped quote: prefix %v, %v", p, err)
	}
}

func TestSameMember(t *testing.T) {
	h, ok := proto.ParseHandle("repo.2+desk.lxc")
	if !ok {
		t.Fatal("handle")
	}
	if err := SameMember(h, h); err != nil {
		t.Fatal(err)
	}
	const want = "the checkout's origin changed; give the entry another seat to take a new handle"
	for _, s := range []string{"other.2+desk.lxc", "repo.3+desk.lxc"} {
		derived, ok := proto.ParseHandle(s)
		if !ok {
			t.Fatal("handle")
		}
		refused(t, s, SameMember(derived, h), want)
	}
}

func TestPasswdNoAccountIsItsOwnRefusal(t *testing.T) {
	_, err := Passwd([]byte("root:x:0:0::/root:/bin/sh\n"), agentUser(t), nil)
	if !errors.Is(err, ErrNoAccount) || !errors.Is(err, ErrRefused) {
		t.Fatalf("err %v", err)
	}
	_, err = Passwd([]byte("root:x:0:0::/root:/bin/sh\nagent:x:0:1002::/srv/agent:/bin/sh\n"), agentUser(t), nil)
	if errors.Is(err, ErrNoAccount) || !errors.Is(err, ErrRefused) {
		t.Fatalf("a uid 0 account: %v", err)
	}
}

func TestParseConfig(t *testing.T) {
	kv := map[string]string{KeyIdmap: "", KeyLink: "lxcbr0", KeyStartAuto: "1"}
	c, err := ParseConfig(kv)
	if err != nil || c != (Config{Link: "lxcbr0", StartAuto: true}) {
		t.Fatalf("config %+v, %v", c, err)
	}
	for _, auto := range []string{"0", ""} {
		kv[KeyStartAuto] = auto
		if c, err := ParseConfig(kv); err != nil || c.StartAuto {
			t.Errorf("start.auto %q: %+v, %v", auto, c, err)
		}
	}
	if len(ConfigKeys) != 3 {
		t.Fatalf("ConfigKeys %v", ConfigKeys)
	}
	for name, c := range map[string]struct {
		kv   map[string]string
		want string
	}{
		"idmap":       {map[string]string{KeyIdmap: "u 0 100000 65536", KeyLink: "br0", KeyStartAuto: "1"}, KeyIdmap},
		"no link":     {map[string]string{KeyIdmap: "", KeyLink: "", KeyStartAuto: "1"}, KeyLink},
		"bad link":    {map[string]string{KeyIdmap: "", KeyLink: "a/b", KeyStartAuto: "1"}, KeyLink},
		"long link":   {map[string]string{KeyIdmap: "", KeyLink: strings.Repeat("a", 16), KeyStartAuto: "1"}, KeyLink},
		"bad auto":    {map[string]string{KeyIdmap: "", KeyLink: "br0", KeyStartAuto: "yes"}, KeyStartAuto},
		"missing key": {map[string]string{KeyLink: "br0", KeyStartAuto: "1"}, KeyIdmap},
		"extra key":   {map[string]string{KeyIdmap: "", KeyLink: "br0", KeyStartAuto: "1", "lxc.other": ""}, "lxc.other"},
	} {
		_, err := ParseConfig(c.kv)
		refused(t, name, err, c.want)
	}
}

func TestSource(t *testing.T) {
	addrs := []netip.Addr{netip.MustParseAddr("fe80::1"), netip.MustParseAddr("198.51.100.7")}
	allow := []string{"192.0.2.0/24", "198.51.100.0/24"}
	if err := Source(addrs, allow, "lxcbr0", "lxcbr0"); err != nil {
		t.Fatal(err)
	}
	mapped := []netip.Addr{netip.MustParseAddr("::ffff:198.51.100.7")}
	if err := Source(mapped, allow, "lxcbr0", "lxcbr0"); err != nil {
		t.Fatalf("a v4-mapped address: %v", err)
	}
	refused(t, "other link", Source(addrs, allow, "virbr0", "lxcbr0"), "bridge")
	refused(t, "outside", Source(addrs, []string{"192.0.2.0/24"}, "lxcbr0", "lxcbr0"), "allow_from")
	refused(t, "no address", Source(nil, allow, "lxcbr0", "lxcbr0"), "no address")
	refused(t, "bad prefix", Source(addrs, []string{"198.51.100.0/33"}, "lxcbr0", "lxcbr0"), "allow_from")
}

func TestStartTicks(t *testing.T) {
	stat := "4242 (tmux: server) (x) S 1 4242 4242 0 -1 4194560 104 0 0 0 0 0 0 0 20 0 1 0 987654 9502720 400 18446744073709551615\n"
	n, err := StartTicks([]byte(stat))
	if err != nil || n != 987654 {
		t.Fatalf("StartTicks = %d, %v", n, err)
	}
	for name, s := range map[string]string{
		"short":      "1 (a) S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0\n",
		"no paren":   "1 a S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 5 0\n",
		"negative":   "1 (a) S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 -5 0\n",
		"word":       "1 (a) S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 x 0\n",
		"overflow":   "1 (a) S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0 99999999999999999999 0\n",
		"two spaces": "1 (a) S 1 1 1 0 -1 0 0 0 0 0 0 0 0 0 20 0 1 0  5 0\n",
		"too long":   "1 (a) S " + strings.Repeat("1 ", MaxStat),
	} {
		_, err := StartTicks([]byte(s))
		refused(t, name, err, "stat")
	}
}
