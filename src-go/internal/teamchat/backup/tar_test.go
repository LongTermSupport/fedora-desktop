package backup

import (
	"archive/tar"
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/apply"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

const alphaTeamJSON = `{"v":1,"team":"alpha","name":"Alpha","server_name":"alpha.teamchat.internal","port":8448,` +
	`"description":"The team that builds the thing.","listen":["192.0.2.10"],"allow_from":["192.0.2.0/24"],` +
	`"url":"http://192.0.2.10:8448","humans":["alice","bob"],"coordinator":"myrepo.1+desk.podman",` +
	`"trust":{"kind":"github","org":"example-org","repos":[{"repo":"example-org/thing","branches":["main"]}]}}`

// alphaPort is alphaTeamJSON's port.
const alphaPort = 8448

const sampleTimeMS = 1792663930000

func alpha(t *testing.T) paths.TeamID {
	t.Helper()
	id, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func sampleSources(t *testing.T) Sources {
	t.Helper()
	team := alpha(t)
	ids := proto.RoomIDs{Main: "!" + strings.Repeat("a", 43), Reports: "!" + strings.Repeat("b", 43)}
	return Sources{
		TeamJSON: []byte(alphaTeamJSON),
		Config:   []byte("server_name = \"alpha.teamchat.internal\"\n"),
		Roster:   apply.NewRosterFile(team).Encode(),
		Rooms:    apply.EncodeRooms(team, ids),
		Token:    []byte("syt_YWRtaW4_abcdefghijklmnop_0123"),
	}
}

func sampleManifest(t *testing.T, n int64) Manifest {
	t.Helper()
	return Manifest{Team: alpha(t), Backup: n, TimeMS: sampleTimeMS, ServerName: "alpha.teamchat.internal"}
}

func sampleTar(t *testing.T, n int64) []byte {
	t.Helper()
	raw, err := Encode(sampleManifest(t, n), sampleSources(t))
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestEncodeDecodeRoundTrips(t *testing.T) {
	raw := sampleTar(t, 3)
	got, err := Decode(raw, alpha(t), 3)
	if err != nil {
		t.Fatal(err)
	}
	src := sampleSources(t)
	if got.Manifest != sampleManifest(t, 3) {
		t.Fatalf("manifest %+v", got.Manifest)
	}
	if got.Team.ID != alpha(t) || got.Team.Port != alphaPort || got.Rooms.Main != "!"+strings.Repeat("a", 43) {
		t.Fatalf("decoded %+v %+v", got.Team, got.Rooms)
	}
	if got.Token != string(src.Token) || !bytes.Equal(got.Config, src.Config) || !bytes.Equal(got.TeamJSON, src.TeamJSON) ||
		!bytes.Equal(got.RosterJSON, src.Roster) || !bytes.Equal(got.RoomsJSON, src.Rooms) || got.Roster.Team != alpha(t) {
		t.Fatal("a member did not come back as written")
	}
	if _, err := Decode(sampleTar(t, 7), alpha(t), 7); err != nil {
		t.Fatal(err)
	}
}

// TestEncodeIsPlainUstar is BK1: six regular 0600 members of uid and gid 0 in order, the
// manifest's time on each, and nothing else, as archive/tar reads them.
func TestEncodeIsPlainUstar(t *testing.T) {
	r := tar.NewReader(bytes.NewReader(sampleTar(t, 3)))
	var names []string
	for {
		h, err := r.Next()
		if err != nil {
			break
		}
		names = append(names, h.Name)
		if h.Typeflag != tar.TypeReg || h.Mode != 0o600 || h.Uid != 0 || h.Gid != 0 || h.Uname != "" || h.Gname != "" ||
			!h.ModTime.Equal(time.UnixMilli(sampleTimeMS)) || len(h.PAXRecords) != 0 {
			t.Fatalf("member %s: %+v", h.Name, h)
		}
	}
	if strings.Join(names, " ") != "backup.json team.json config.toml roster.json rooms.json admin.token" {
		t.Fatalf("members %v", names)
	}
	m := string(memberData(t, sampleTar(t, 3), "backup.json"))
	if m != `{"v":1,"team":"alpha","backup":3,"time_ms":1792663930000,"server_name":"alpha.teamchat.internal"}` {
		t.Fatalf("manifest %s", m)
	}
}

func memberData(t *testing.T, raw []byte, name string) []byte {
	t.Helper()
	r := tar.NewReader(bytes.NewReader(raw))
	for {
		h, err := r.Next()
		if err != nil {
			t.Fatalf("no member %s: %v", name, err)
		}
		if h.Name == name {
			var b bytes.Buffer
			if _, err := b.ReadFrom(r); err != nil {
				t.Fatal(err)
			}
			return b.Bytes()
		}
	}
}

func TestEncodeRefusesOversizedSources(t *testing.T) {
	cases := map[string]func(*Sources){
		"token":     func(s *Sources) { s.Token = bytes.Repeat([]byte("a"), 4097) },
		"team.json": func(s *Sources) { s.TeamJSON = bytes.Repeat([]byte("a"), 64<<10+1) },
		"config":    func(s *Sources) { s.Config = bytes.Repeat([]byte("a"), 64<<10+1) },
		"roster":    func(s *Sources) { s.Roster = bytes.Repeat([]byte("a"), 1<<20+1) },
		"rooms":     func(s *Sources) { s.Rooms = bytes.Repeat([]byte("a"), 1<<20+1) },
	}
	for name, change := range cases {
		t.Run(name, func(t *testing.T) {
			s := sampleSources(t)
			change(&s)
			if _, err := Encode(sampleManifest(t, 1), s); err == nil {
				t.Fatal("encoded")
			}
		})
	}
	m := sampleManifest(t, 1)
	m.TimeMS = 1500
	if _, err := Encode(m, sampleSources(t)); err == nil {
		t.Fatal("encoded a time that is not whole seconds")
	}
}

// member is one entry of a hand-built tar.
type member struct {
	h    tar.Header
	data []byte
}

// goodMembers are sampleTar's members, for a test to change before writing them with archive/tar.
func goodMembers(t *testing.T) []member {
	t.Helper()
	raw := sampleTar(t, 3)
	r := tar.NewReader(bytes.NewReader(raw))
	var out []member
	for {
		h, err := r.Next()
		if err != nil {
			return out
		}
		var b bytes.Buffer
		if _, err := b.ReadFrom(r); err != nil {
			t.Fatal(err)
		}
		out = append(out, member{h: *h, data: b.Bytes()})
	}
}

func writeTar(t *testing.T, members []member) []byte {
	t.Helper()
	var b bytes.Buffer
	w := tar.NewWriter(&b)
	for _, m := range members {
		h := m.h
		if err := w.WriteHeader(&h); err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write(m.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

func TestHandBuiltGoodTarDecodes(t *testing.T) {
	if _, err := Decode(writeTar(t, goodMembers(t)), alpha(t), 3); err != nil {
		t.Fatal(err)
	}
}

// TestDecodeRefusesHostileTars is BK3 and C 3.14's hostile tars.
func TestDecodeRefusesHostileTars(t *testing.T) {
	for name, mutate := range hostileTars() {
		t.Run(name, func(t *testing.T) {
			raw := writeTar(t, mutate(goodMembers(t)))
			_, err := Decode(raw, alpha(t), 3)
			if err == nil {
				t.Fatal("decoded")
			}
			t.Log(err)
		})
	}
}

func hostileTars() map[string]func([]member) []member {
	change := func(i int, f func(*member)) func([]member) []member {
		return func(ms []member) []member {
			f(&ms[i])
			return ms
		}
	}
	cases := map[string]func([]member) []member{
		"a link": change(1, func(m *member) {
			m.h.Typeflag, m.h.Linkname, m.h.Size, m.data = tar.TypeSymlink, "/etc/shadow", 0, nil
		}),
		"a hard link": change(1, func(m *member) {
			m.h.Typeflag, m.h.Linkname, m.h.Size, m.data = tar.TypeLink, "backup.json", 0, nil
		}),
		"a device": change(2, func(m *member) {
			m.h.Typeflag, m.h.Devmajor, m.h.Size, m.data = tar.TypeChar, 1, 0, nil
		}),
		"a directory": change(2, func(m *member) {
			m.h.Typeflag, m.h.Size, m.data = tar.TypeDir, 0, nil
		}),
		"dot dot":      change(5, func(m *member) { m.h.Name = "../admin.token" }),
		"setuid":       change(5, func(m *member) { m.h.Mode = 0o4600 }),
		"sticky":       change(5, func(m *member) { m.h.Mode = 0o1600 }),
		"mode 0644":    change(4, func(m *member) { m.h.Mode = 0o644 }),
		"uid 1000":     change(3, func(m *member) { m.h.Uid = 1000 }),
		"gid 1000":     change(3, func(m *member) { m.h.Gid = 1000 }),
		"a user name":  change(3, func(m *member) { m.h.Uname = "root" }),
		"a group name": change(3, func(m *member) { m.h.Gname = "root" }),
		"another time": change(3, func(m *member) { m.h.ModTime = m.h.ModTime.Add(time.Second) }),
		"pax records": change(3, func(m *member) {
			m.h.Format, m.h.PAXRecords = tar.FormatPAX, map[string]string{"comment": "x"}
		}),
		"an xattr": change(3, func(m *member) {
			m.h.Format, m.h.PAXRecords = tar.FormatPAX, map[string]string{"SCHILY.xattr.user.x": "y"}
		}),
		"gnu format": change(3, func(m *member) { m.h.Format = tar.FormatGNU }),
		"out of order": func(ms []member) []member {
			ms[1], ms[2] = ms[2], ms[1]
			return ms
		},
		"given twice":    func(ms []member) []member { return append(ms[:5], ms[4]) },
		"an extra":       func(ms []member) []member { return append(ms, member{h: ms[5].h, data: ms[5].data}) },
		"one missing":    func(ms []member) []member { return ms[:5] },
		"unknown member": change(2, func(m *member) { m.h.Name = "evil.sh" }),
		"oversize token": change(5, func(m *member) {
			m.data = bytes.Repeat([]byte("a"), 4097)
			m.h.Size = int64(len(m.data))
		}),
		"a bad token": change(5, func(m *member) {
			m.data = []byte("has space")
			m.h.Size = int64(len(m.data))
		}),
		"a bad roster": change(3, func(m *member) {
			m.data = []byte(`{"v":1}`)
			m.h.Size = int64(len(m.data))
		}),
		"bad rooms": change(4, func(m *member) {
			m.data = []byte(`{"v":1,"team":"beta"}`)
			m.h.Size = int64(len(m.data))
		}),
		"a bad team file": change(1, func(m *member) {
			m.data = []byte(`{"v":2}`)
			m.h.Size = int64(len(m.data))
		}),
		"a config with nul": change(2, func(m *member) {
			m.data = []byte("a\x00b")
			m.h.Size = int64(len(m.data))
		}),
		"a config not utf-8": change(2, func(m *member) {
			m.data = []byte("a\xffb")
			m.h.Size = int64(len(m.data))
		}),
	}
	return cases
}

func manifestMember(t *testing.T, text string) []member {
	t.Helper()
	ms := goodMembers(t)
	ms[0].data = []byte(text)
	ms[0].h.Size = int64(len(text))
	return ms
}

func TestDecodeRefusesABadManifest(t *testing.T) {
	cases := map[string]string{
		"another team":    `{"v":1,"team":"beta","backup":3,"time_ms":1792663930000,"server_name":"alpha.teamchat.internal"}`,
		"another number":  `{"v":1,"team":"alpha","backup":4,"time_ms":1792663930000,"server_name":"alpha.teamchat.internal"}`,
		"another server":  `{"v":1,"team":"alpha","backup":3,"time_ms":1792663930000,"server_name":"beta.teamchat.internal"}`,
		"version 2":       `{"v":2,"team":"alpha","backup":3,"time_ms":1792663930000,"server_name":"alpha.teamchat.internal"}`,
		"an extra key":    `{"v":1,"team":"alpha","backup":3,"time_ms":1792663930000,"server_name":"alpha.teamchat.internal","x":1}`,
		"a missing key":   `{"v":1,"team":"alpha","backup":3,"time_ms":1792663930000}`,
		"a string number": `{"v":1,"team":"alpha","backup":"3","time_ms":1792663930000,"server_name":"alpha.teamchat.internal"}`,
		"not json":        `{"v":1,`,
		"not an object":   `[1]`,
		"time in ms":      `{"v":1,"team":"alpha","backup":3,"time_ms":1792663930001,"server_name":"alpha.teamchat.internal"}`,
		"a bad team":      `{"v":1,"team":"Alpha!","backup":3,"time_ms":1792663930000,"server_name":"alpha.teamchat.internal"}`,
	}
	for name, text := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(writeTar(t, manifestMember(t, text)), alpha(t), 3); err == nil {
				t.Fatal("decoded")
			}
		})
	}
}

// TestDecodeRefusesBrokenFraming covers what archive/tar would not write: a wrong checksum, a
// header field that is not octal, a short archive, bytes after the end blocks, and the cap.
func TestDecodeRefusesBrokenFraming(t *testing.T) {
	good := sampleTar(t, 3)
	cases := map[string]func([]byte) []byte{
		"a wrong checksum":    func(b []byte) []byte { b[148]++; return b },
		"a size not octal":    func(b []byte) []byte { b[124] = '9'; return fixSum(b) },
		"base-256 size":       func(b []byte) []byte { b[124] = 0x80; return fixSum(b) },
		"a prefix":            func(b []byte) []byte { b[345] = 'x'; return fixSum(b) },
		"a link name":         func(b []byte) []byte { b[157] = 'x'; return fixSum(b) },
		"a wrong magic":       func(b []byte) []byte { b[262] = ' '; return fixSum(b) },
		"padding not zero":    func(b []byte) []byte { b[500] = 'x'; return fixSum(b) },
		"a device number":     func(b []byte) []byte { copy(b[329:337], "0000001\x00"); return fixSum(b) },
		"a name not padded":   func(b []byte) []byte { b[20] = 'x'; return fixSum(b) },
		"data padding":        func(b []byte) []byte { b[512+200] = 'x'; return b },
		"truncated":           func(b []byte) []byte { return b[:len(b)-1024-10] },
		"no end blocks":       func(b []byte) []byte { return b[:len(b)-1024] },
		"one end block":       func(b []byte) []byte { return b[:len(b)-512] },
		"data after the end":  func(b []byte) []byte { return append(b, 'x') },
		"zeros after the end": func(b []byte) []byte { return append(b, make([]byte, 512)...) },
		"over 4 MiB":          func(b []byte) []byte { return append(b, make([]byte, 4<<20)...) },
		"empty":               func([]byte) []byte { return nil },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode(mutate(bytes.Clone(good)), alpha(t), 3)
			if err == nil {
				t.Fatal("decoded")
			}
			t.Log(err)
		})
	}
}

// fixSum recomputes the first header's checksum, so a test reaches the check after it.
func fixSum(b []byte) []byte {
	h := b[:512]
	copy(h[148:156], "        ")
	var sum int64
	for _, c := range h {
		sum += int64(c)
	}
	copy(h[148:156], []byte(octal(sum, 6)+"\x00 "))
	return b
}

func TestDecodeErrorsQuoteNoContent(t *testing.T) {
	ms := goodMembers(t)
	ms[2].h.Name = "SECRET-NAME"
	ms[5].data = []byte("SECRET TOKEN")
	ms[5].h.Size = int64(len(ms[5].data))
	_, err := Decode(writeTar(t, ms), alpha(t), 3)
	if err == nil || strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("err %v", err)
	}
	ms = goodMembers(t)
	ms[5].data = []byte("SECRET TOKEN")
	ms[5].h.Size = int64(len(ms[5].data))
	_, err = Decode(writeTar(t, ms), alpha(t), 3)
	if err == nil || strings.Contains(err.Error(), "SECRET") || !strings.Contains(err.Error(), "admin.token") {
		t.Fatalf("err %v", err)
	}
}
