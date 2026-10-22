package seat

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// memberCase is one member.json and what config.read_member makes of it (V32): want is the
// refusal after "team alpha: <path>: ", "" for accepted (with handle, testHandle when empty).
type memberCase struct {
	about  string
	doc    string
	setup  func(f fx, dir string) // builds member.json itself; doc is then unused
	want   string
	handle string
	// notOn311 marks a case python 3.11 (the container) answers differently from the host's
	// 3.13+, which the Go follows (SPEC-view B-S5); the parity test sets it aside.
	notOn311 bool
}

const limitKeys = "send_per_minute, send_burst, recv_per_sender_minute, ack_timeout_s, " +
	"halt_ack_timeout_s, human_max_age_s, wait_timeout_s, report_per_hour"

func refuse(key, why string) string { return key + " " + why }

func memberCases() []memberCase {
	groups := [][]memberCase{
		fileCases(), decodeCases(), keyCases(), fieldCases(), userCases(), hostCases(), limitCases(), urlCases(),
	}
	var count int
	for _, g := range groups {
		count += len(g)
	}
	cases := make([]memberCase, 0, count)
	for _, g := range groups {
		cases = append(cases, g...)
	}
	return cases
}

// member.json as a file: what it is, how it is reached, its size.
func fileCases() []memberCase {
	valid := memberDoc(testTeam)
	at := func(dir string) string { return filepath.Join(dir, memberFile) }
	return []memberCase{
		{about: "valid", doc: valid},
		{about: "missing", setup: func(fx, string) {}, want: "no bundle (member.json is missing)"},
		// python's fdopen refuses a directory (EISDIR) before the regular-file check.
		{about: "a directory", setup: func(f fx, dir string) { f.mkdirs(at(dir)) }, want: "member.json cannot be read (Is a directory)"},
		{about: "a fifo", setup: func(f fx, dir string) { f.mkfifo(at(dir)) }, want: "member.json is not a regular file"},
		{about: "a socket", setup: func(f fx, dir string) { f.mksock(at(dir)) }, want: "member.json cannot be read (No such device or address)"},
		{about: "a symlink to a valid file", setup: func(f fx, dir string) {
			f.writeFile(filepath.Join(dir, "real.json"), valid)
			f.symlink("real.json", at(dir))
		}},
		{about: "a dangling symlink", setup: func(f fx, dir string) { f.symlink("nowhere.json", at(dir)) }, want: "no bundle (member.json is missing)"},
		{
			about: "a symlink loop", setup: func(f fx, dir string) { f.symlink(memberFile, at(dir)) },
			want: "member.json cannot be read (Too many levels of symbolic links)",
		},
		{about: "65536 bytes", doc: valid + strings.Repeat(" ", 65536-len(valid))},
		{about: "65537 bytes", doc: valid + strings.Repeat(" ", 65537-len(valid)), want: "member.json is larger than 65536 bytes"},
	}
}

func decodeCases() []memberCase {
	valid := memberDoc(testTeam)
	notJSON := func(class string) string { return "member.json is not valid JSON (" + class + ")" }
	return []memberCase{
		{about: "empty", doc: "", want: notJSON("JSONDecodeError")},
		{about: "not JSON", doc: "{protocol: 1}", want: notJSON("JSONDecodeError")},
		{about: "bad UTF-8", doc: "{\"team\": \"a\xff\"}", want: notJSON("UnicodeDecodeError")},
		{about: "an encoded surrogate", doc: "{\"team\": \"a\xed\xa0\x80\"}", want: notJSON("UnicodeDecodeError")},
		{about: "a BOM", doc: "\xef\xbb\xbf" + valid, want: notJSON("JSONDecodeError")},
		{about: "extra data", doc: valid + " x", want: notJSON("JSONDecodeError")},
		{about: "a duplicate key", doc: memberDoc(testTeam, also("protocol", "1")), want: notJSON("ValueError")},
		{about: "a nested duplicate key", doc: memberDoc(testTeam, with("limits", `{"send_burst": 2, "send_burst": 3}`)), want: notJSON("ValueError")},
		{about: "an integer of 5000 digits", doc: memberDoc(testTeam, with("protocol", strings.Repeat("7", 5000))), want: notJSON("ValueError")},
		{about: "an array", doc: "[" + valid + "]", want: "member.json is not a JSON object"},
		{about: "a string", doc: `"x"`, want: "member.json is not a JSON object"},
		{about: "null", doc: "null", want: "member.json is not a JSON object"},
	}
}

func keyCases() []memberCase {
	k32, k33 := strings.Repeat("k", 32), strings.Repeat("k", 33)
	unknown := func(labels string) string { return "member.json has unknown keys: " + labels }
	cases := []memberCase{
		{about: "unknown keys", doc: memberDoc(testTeam, also("zeta", "1"), also("extra", "2")), want: unknown("'extra', 'zeta'")},
		{about: "an unknown key that is not a name", doc: memberDoc(testTeam, also("Bad Key", "1")), want: unknown("a key that is not a name")},
		{about: "a 32-letter unknown key", doc: memberDoc(testTeam, also(k32, "1")), want: unknown("'" + k32 + "'")},
		{about: "a 33-letter unknown key", doc: memberDoc(testTeam, also(k33, "1")), want: unknown("a key that is not a name")},
		{about: "six unknown keys", doc: memberDoc(testTeam, also("f", "1"), also("e", "1"), also("d", "1"),
			also("c", "1"), also("b", "1"), also("a", "1")), want: unknown("'a', 'b', 'c', 'd', 'e'")},
		{
			about: "unknown keys sort by code point", doc: memberDoc(testTeam, also("é", "1"), also("Z", "1"), also("z", "1")),
			want: unknown("a key that is not a name, 'z', a key that is not a name"),
		},
		{about: "unknown before missing", doc: memberDoc(testTeam, without("room"), also("zz", "1")), want: unknown("'zz'")},
		{about: "missing protocol before room", doc: memberDoc(testTeam, without("room"), without("protocol")), want: refuse("protocol", "is missing")},
	}
	missing := make([]memberCase, 0, len(requiredKeys))
	for _, key := range requiredKeys {
		missing = append(missing, memberCase{about: "missing " + key, doc: memberDoc(testTeam, without(key)), want: refuse(key, "is missing")})
	}
	return slices.Concat(cases, missing)
}

func fieldCases() []memberCase {
	one := refuse("protocol", "must be the integer 1")
	notTeam := refuse("team", "does not equal the bundle directory's name (alpha)")
	notDNS := refuse("server_name", "is not a lowercase DNS name")
	notBool := refuse("human_text", "must be true or false")
	field := func(key, raw string) string { return memberDoc(testTeam, with(key, raw)) }
	return []memberCase{
		{about: "human_text true", doc: field("human_text", "true")},
		{about: "human_text false", doc: field("human_text", "false")},
		{about: "human_text a string", doc: field("human_text", `"yes"`), want: notBool},
		{about: "human_text 1", doc: field("human_text", "1"), want: notBool},
		{about: "human_text null", doc: field("human_text", "null"), want: notBool},
		{about: "protocol true", doc: field("protocol", "true"), want: one},
		{about: "protocol 1.0", doc: field("protocol", "1.0"), want: one},
		{about: "protocol 2", doc: field("protocol", "2"), want: one},
		{about: "protocol a string", doc: field("protocol", `"1"`), want: one},
		{about: "protocol NaN", doc: field("protocol", "NaN"), want: one},
		{about: "protocol before team", doc: memberDoc(testTeam, with("protocol", "0"), with("team", `"beta"`)), want: one},
		{about: "another team", doc: field("team", `"beta"`), want: notTeam},
		{about: "team a number", doc: field("team", "1"), want: notTeam},
		{about: "server_name upper case", doc: field("server_name", js("Bus.example.com")), want: notDNS},
		{about: "server_name empty", doc: field("server_name", `""`), want: notDNS},
		{about: "server_name a number", doc: field("server_name", "5"), want: notDNS},
		{about: "server_name an empty label", doc: field("server_name", js("bus..example.com")), want: notDNS},
		{about: "server_name a leading hyphen", doc: field("server_name", js("-bus.example.com")), want: notDNS},
		{about: "server_name a 64-character label", doc: field("server_name", js(strings.Repeat("a", 64)+".example.com")), want: notDNS},
		serverNamed(253, ""),
		serverNamed(254, notDNS),
		{about: "admin on another server", doc: field("admin", js("@admin:other.example.com")), want: refuse("admin", "is not @admin:<server_name>")},
		{about: "admin null", doc: field("admin", "null"), want: refuse("admin", "is not @admin:<server_name>")},
		{about: "room too short", doc: field("room", js("!short")), want: refuse("room", "is not a room ID")},
		{about: "room a number", doc: field("room", "5"), want: refuse("room", "is not a room ID")},
		{about: "room with a bad character", doc: field("room", js("!"+strings.Repeat("A", 42)+".")), want: refuse("room", "is not a room ID")},
		{
			about: "token_file another name", doc: field("token_file", js("token2")),
			want: refuse("token_file", `must be "token", the file inside the bundle`),
		},
	}
}

func userCases() []memberCase {
	wrong := refuse("user_id", "is not @<handle>:<server_name> for this server_name")
	user := func(raw string) string { return memberDoc(testTeam, with("user_id", raw)) }
	return []memberCase{
		{about: "user_id on another server", doc: user(js("@" + testHandle + ":other.example.com")), want: wrong},
		{about: "user_id without @", doc: user(js(testHandle + ":" + testServer)), want: wrong},
		{about: "user_id without a server", doc: user(js("@" + testHandle)), want: wrong},
		{about: "user_id a human", doc: user(js("@alice:" + testServer)), want: wrong},
		{about: "user_id the admin", doc: user(js("@admin:" + testServer)), want: wrong},
		{about: "user_id a number", doc: user("7"), want: wrong},
		{about: "user_id an unknown type", doc: user(js("@demo.1+box.pod:" + testServer)), want: wrong},
		{about: "user_id seat 0", doc: user(js("@demo.0+box.podman:" + testServer)), want: wrong},
		{about: "user_id a named seat", doc: user(js("@demo-x.seat1+box-2.lxc:" + testServer)), handle: "demo-x.seat1+box-2.lxc"},
	}
}

func hostCases() []memberCase {
	only := refuse("plain_http_hosts", "must hold only IP literals")
	canonical := refuse("plain_http_hosts", "must hold IP literals in canonical form")
	hosts := func(raw string) string { return memberDoc(testTeam, with("plain_http_hosts", raw)) }
	return []memberCase{
		{about: "plain_http_hosts a string", doc: hosts(js("192.0.2.1")), want: refuse("plain_http_hosts", "must be an array of IP literals")},
		{about: "plain_http_hosts an object", doc: hosts("{}"), want: refuse("plain_http_hosts", "must be an array of IP literals")},
		{about: "plain_http_hosts empty, https", doc: hosts("[]")},
		{about: "plain_http_hosts a number", doc: hosts("[1]"), want: only},
		{about: "plain_http_hosts a name", doc: hosts(`["bus.example.com"]`), want: only},
		{about: "plain_http_hosts a leading zero", doc: hosts(`["192.0.2.01"]`), want: only},
		{about: "plain_http_hosts a scope", doc: hosts(`["2001:db8::1%eth0"]`), want: only},
		{about: "plain_http_hosts upper case", doc: hosts(`["2001:DB8::1"]`), want: canonical},
		{about: "plain_http_hosts uncompressed", doc: hosts(`["2001:db8:0:0::1"]`), want: canonical},
		{about: "plain_http_hosts twice", doc: hosts(`["192.0.2.1", "192.0.2.1"]`), want: refuse("plain_http_hosts", "lists an address twice")},
		{about: "plain_http_hosts IPv4-mapped, dotted", doc: hosts(`["::ffff:192.0.2.1"]`), notOn311: true},
		{about: "plain_http_hosts IPv4-mapped, hex", doc: hosts(`["::ffff:c000:201"]`), want: canonical, notOn311: true},
	}
}

func limitCases() []memberCase {
	limits := func(raw string) string { return memberDoc(testTeam, with("limits", raw)) }
	cases := []memberCase{
		{about: "limits null", doc: limits("null"), want: "limits must be an object"},
		{about: "limits an array", doc: limits("[]"), want: "limits must be an object"},
		{about: "limits an unknown key", doc: limits(`{"send_burst": 2, "bogus": 1}`), want: "limits may hold only these keys: " + limitKeys},
		{about: "limits empty", doc: limits("{}")},
		{about: "limits every bound", doc: limits(`{"send_per_minute": 60, "send_burst": 1, "recv_per_sender_minute": 600, ` +
			`"ack_timeout_s": 60, "halt_ack_timeout_s": 3600, "human_max_age_s": 3600, "wait_timeout_s": 1790, "report_per_hour": 60}`)},
		{about: "limits report_per_hour too high", doc: limits(`{"report_per_hour": 61}`), want: "limits.report_per_hour must be an integer from 1 to 60"},
		{
			about: "limits the first bad in file order", doc: limits(`{"send_burst": 0, "send_per_minute": 0}`),
			want: "limits.send_burst must be an integer from 1 to 30",
		},
		{about: "limits a bool", doc: limits(`{"send_burst": true}`), want: "limits.send_burst must be an integer from 1 to 30"},
		{about: "limits a float", doc: limits(`{"send_burst": 5.0}`), want: "limits.send_burst must be an integer from 1 to 30"},
		{about: "limits a string", doc: limits(`{"ack_timeout_s": "60"}`), want: "limits.ack_timeout_s must be an integer from 60 to 86400"},
		{
			about: "limits a huge integer", doc: limits(`{"ack_timeout_s": 99999999999999999999}`),
			want: "limits.ack_timeout_s must be an integer from 60 to 86400",
		},
	}
	bounds := make([]memberCase, 0, 2*len(limitBounds))
	for _, b := range limitBounds {
		want := fmt.Sprintf("limits.%s must be an integer from %d to %d", b.key, b.low, b.high)
		bounds = append(bounds,
			memberCase{about: "limits under " + b.key, doc: limits(fmt.Sprintf(`{%q: %d}`, b.key, b.low-1)), want: want},
			memberCase{about: "limits over " + b.key, doc: limits(fmt.Sprintf(`{%q: %d}`, b.key, b.high+1)), want: want})
	}
	return slices.Concat(cases, bounds)
}

func urlCases() []memberCase {
	cases := make([]memberCase, 0, len(baseURLCases))
	for _, u := range baseURLCases {
		cases = append(cases, memberCase{about: "base_url " + u.raw, doc: memberDoc(testTeam, with("base_url", u.raw)), want: u.want})
	}
	return cases
}

// serverNamed is a member on a server_name of n characters, user_id and admin to match.
func serverNamed(n int, want string) memberCase {
	labels := make([]string, 0, n/50+1)
	for left := n; left > 0; {
		size := min(49, left)
		labels = append(labels, strings.Repeat("s", size))
		left -= size + 1
		if left == 0 { // the separator took the last character: a one-letter label fills it
			labels[len(labels)-1] = labels[len(labels)-1][1:]
			labels = append(labels, "s")
		}
	}
	server := strings.Join(labels, ".")
	if len(server) != n {
		panic(fmt.Sprintf("server name of %d, not %d", len(server), n))
	}
	return memberCase{about: fmt.Sprintf("server_name of %d characters", n), want: want, doc: memberDoc(testTeam,
		with("server_name", js(server)), with("user_id", js("@"+testHandle+":"+server)), with("admin", js("@admin:"+server)))}
}

// build makes the case's member.json in dir.
func (c memberCase) build(f fx, dir string) {
	f.t.Helper()
	if c.setup != nil {
		c.setup(f, dir)
		return
	}
	f.writeFile(filepath.Join(dir, memberFile), c.doc)
}

func TestReadMember(t *testing.T) {
	top := newCheckout(t)
	for i, c := range memberCases() {
		t.Run(c.about, func(t *testing.T) {
			f := fx{t}
			dir := f.addSeat(top, testTeam, fmt.Sprintf("m%03d", i))
			c.build(f, dir)
			handle, err := readMember(testTeam, dir)
			if c.want == "" {
				want := c.handle
				if want == "" {
					want = testHandle
				}
				if err != nil || handle != want {
					t.Fatalf("got %q, %v; want %q", handle, err, want)
				}
				return
			}
			refusal, ok := errors.AsType[*RefusalError](err)
			wantMsg := "team alpha: " + filepath.Join(dir, memberFile) + ": " + c.want
			if !ok || refusal.Msg != wantMsg {
				t.Fatalf("got %q, %v\nwant refusal %q", handle, err, wantMsg)
			}
		})
	}
}

// crashDocs are member.json documents v1 did not survive: JSON nested past python's recursion
// limit (RecursionError) and each base_url urlsplit raises ValueError for (SPEC-view B-S3).
func crashDocs() []string {
	docs := make([]string, 0, 1+len(crashCases))
	docs = append(docs, memberDoc(testTeam, with("limits", strings.Repeat("[", 30000)+strings.Repeat("]", 30000))))
	for _, raw := range crashCases {
		docs = append(docs, memberDoc(testTeam, with("base_url", js(raw))))
	}
	return docs
}

func TestReadMemberCrashesWhereV1Crashed(t *testing.T) {
	f := fx{t}
	top := newCheckout(t)
	for i, doc := range crashDocs() {
		dir := f.addSeat(top, testTeam, fmt.Sprintf("c%d", i))
		f.writeFile(filepath.Join(dir, memberFile), doc)
		if _, err := readMember(testTeam, dir); err == nil || isRefusal(err) {
			t.Errorf("case %d: got %v, want a crash-class error", i, err)
		}
	}
}

func TestReadMemberUnreadableBundle(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads a mode-0000 file")
	}
	f := fx{t}
	dir := f.addSeat(newCheckout(t), testTeam, "1")
	path := filepath.Join(dir, memberFile)
	f.writeFile(path, memberDoc(testTeam))
	f.check(os.Chmod(path, 0))
	_, err := readMember(testTeam, dir)
	refusal, ok := errors.AsType[*RefusalError](err)
	if !ok || !strings.HasSuffix(refusal.Msg, "member.json cannot be read (Permission denied)") {
		t.Fatalf("got %v", err)
	}
}
