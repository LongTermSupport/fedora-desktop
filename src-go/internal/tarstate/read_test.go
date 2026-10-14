package tarstate

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"errors"
	"io"
	"slices"
	"strconv"
	"strings"
	"testing"
)

type member struct {
	hdr  tar.Header
	data string
}

func file(name, data string) member {
	return member{hdr: tar.Header{Name: name, Typeflag: tar.TypeReg, Mode: 0o600, Size: int64(len(data))}, data: data}
}

func dir(name string) member {
	return member{hdr: tar.Header{Name: name, Typeflag: tar.TypeDir, Mode: 0o700}}
}

func symlink(name, target string) member {
	return member{hdr: tar.Header{Name: name, Typeflag: tar.TypeSymlink, Linkname: target, Mode: 0o777}}
}

func special(name string, typeflag byte) member {
	return member{hdr: tar.Header{Name: name, Typeflag: typeflag, Mode: 0o600}}
}

func build(t *testing.T, members ...member) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, m := range members {
		hdr := m.hdr
		if err := w.WriteHeader(&hdr); err != nil {
			t.Fatalf("WriteHeader(%q): %v", hdr.Name, err)
		}
		if _, err := io.WriteString(w, m.data); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func good() []member {
	return []member{
		file("team.json", `{"team":"a"}`),
		file("registry.json", `{}`),
		file("room_id", "!r:example.com"),
		dir("secrets"),
		file("secrets/registration_shared_secret", "s3cret"),
	}
}

func problems(t *testing.T, raw []byte) []string {
	t.Helper()
	_, err := Read(bytes.NewReader(raw), DefaultLimits())
	if err == nil {
		t.Fatal("tar accepted")
	}
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("error %v is not ErrRefused (exit 78 class)", err)
	}
	var refused *RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("error %v carries no problem list", err)
	}
	return refused.Problems
}

func hasProblem(list []string, want string) bool {
	for _, p := range list {
		if strings.Contains(p, want) {
			return true
		}
	}
	return false
}

func TestReadAcceptsAStateTar(t *testing.T) {
	s, err := Read(bytes.NewReader(build(t, good()...)), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if data, ok := s.File("team.json"); !ok || string(data) != `{"team":"a"}` {
		t.Fatalf("team.json = %q, %v", data, ok)
	}
	if !s.Has("secrets") || !s.Has("room_id") || s.Has("nothing") {
		t.Fatal("Has is wrong")
	}
	if _, ok := s.File("secrets"); ok {
		t.Fatal("a directory answered as a file")
	}
}

func TestReadAcceptsTheMinimum(t *testing.T) {
	if _, err := Read(bytes.NewReader(build(t, file("team.json", "{}"), dir("secrets"))), DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

func TestReadAcceptsSecretsLayouts(t *testing.T) {
	cases := map[string][]member{
		"nested directory":      {dir("secrets/sub"), file("secrets/sub/key", "k")},
		"deep file with parent": {file("secrets/a/b/c", "k")},
		"dir with trailing slash": {
			{hdr: tar.Header{Name: "secrets/sub/", Typeflag: tar.TypeDir, Mode: 0o700}},
		},
		"name with space":   {file("secrets/a b", "k")},
		"unicode name":      {file("secrets/clé-ключ", "k")},
		"dotfile":           {file("secrets/.hidden", "k")},
		"dots inside name":  {file("secrets/a..b", "k")},
		"empty file":        {file("secrets/empty", "")},
		"world writable":    {{hdr: tar.Header{Name: "secrets/w", Typeflag: tar.TypeReg, Mode: 0o666}}},
		"backslash in name": {file(`secrets/a\b`, "k")},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			members := append([]member{file("team.json", "{}"), dir("secrets")}, extra...)
			if _, err := Read(bytes.NewReader(build(t, members...)), DefaultLimits()); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReadAcceptsALongNameInsideSecrets(t *testing.T) {
	name := "secrets/" + strings.Repeat("a", 150)
	members := []member{file("team.json", "{}"), dir("secrets"), file(name, "k")}
	if _, err := Read(bytes.NewReader(build(t, members...)), DefaultLimits()); err != nil {
		t.Fatal(err)
	}
}

// Every member name that is not one of the state files is refused with the problem line the
// bash printed.
func TestReadRefusesNames(t *testing.T) {
	names := []string{
		"etc/passwd",
		"/etc/passwd",
		"/team.json",
		"../team.json",
		"./team.json",
		"team.json.bak",
		"team.jsonx",
		"TEAM.JSON",
		"registry.json2",
		"room_id/x",
		"foo",
		"secretsx/a",
		"Secrets/a",
		"secrets/../../x",
		"secrets/../x",
		"secrets/./x",
		"secrets//x",
		"secrets/x/..",
		"secrets/..",
		"secrets/.",
		"/secrets/a",
		"./secrets/a",
		"x/secrets/a",
		"secrets/a/../../../etc/cron.d/x",
		"secrets" + "\x01" + "/a",
		"secrets/a\nsecrets/b",
		"secrets/a\x1b[31m",
		"secrets/a\x7f",
	}
	for _, name := range names {
		t.Run(strconv.Quote(name), func(t *testing.T) {
			raw := build(t, file("team.json", "{}"), dir("secrets"), file(name, "x"))
			got := problems(t, raw)
			if !hasProblem(got, "not one of the state files") {
				t.Fatalf("problems %q lack the not-a-state-file line", got)
			}
		})
	}
}

func TestReadRefusesAFileNamedSecretsAndDirsOutsideSecrets(t *testing.T) {
	cases := map[string][]member{
		"file named secrets":     {file("team.json", "{}"), file("secrets", "x")},
		"dir named team.json":    {dir("team.json"), dir("secrets")},
		"dir named registry":     {file("team.json", "{}"), dir("secrets"), dir("registry.json")},
		"dir named room_id":      {file("team.json", "{}"), dir("secrets"), dir("room_id")},
		"dir etc":                {file("team.json", "{}"), dir("secrets"), dir("etc")},
		"dir named dot":          {file("team.json", "{}"), dir("secrets"), dir(".")},
		"dir named root":         {file("team.json", "{}"), dir("secrets"), dir("/")},
		"dir below secrets dots": {file("team.json", "{}"), dir("secrets"), dir("secrets/../etc")},
	}
	for name, members := range cases {
		t.Run(name, func(t *testing.T) {
			if got := problems(t, build(t, members...)); !hasProblem(got, "not one of the state files") {
				t.Fatalf("problems %q", got)
			}
		})
	}
}

// Anything that is not a plain file or directory is refused by type, whatever its name.
func TestReadRefusesMemberTypes(t *testing.T) {
	cases := map[string]member{
		"symlink in secrets":          symlink("secrets/link", "../team.json"),
		"symlink to absolute":         symlink("secrets/link", "/etc/shadow"),
		"symlink team.json":           symlink("team.json", "/etc/shadow"),
		"symlink secrets":             symlink("secrets", "/etc"),
		"symlink registry":            symlink("registry.json", "../x"),
		"symlink with empty target":   symlink("secrets/link", ""),
		"hardlink in secrets":         {hdr: tar.Header{Name: "secrets/h", Typeflag: tar.TypeLink, Linkname: "/etc/shadow", Mode: 0o600}},
		"hardlink to team.json":       {hdr: tar.Header{Name: "secrets/h", Typeflag: tar.TypeLink, Linkname: "team.json", Mode: 0o600}},
		"hardlink named like a state": {hdr: tar.Header{Name: "room_id", Typeflag: tar.TypeLink, Linkname: "/etc/passwd", Mode: 0o600}},
		"char device":                 special("secrets/dev", tar.TypeChar),
		"block device":                special("secrets/dev", tar.TypeBlock),
		"device named team.json":      special("team.json", tar.TypeChar),
		"fifo":                        special("secrets/fifo", tar.TypeFifo),
		"contiguous file":             special("secrets/c", tar.TypeCont),
		"global pax header as member": {hdr: tar.Header{Name: "secrets/g", Typeflag: tar.TypeXGlobalHeader, PAXRecords: map[string]string{"comment": "x"}}},
	}
	for name, m := range cases {
		t.Run(name, func(t *testing.T) {
			raw := build(t, file("team.json", "{}"), dir("secrets"), m)
			if name == "symlink team.json" || name == "symlink secrets" || name == "device named team.json" {
				raw = build(t, m)
			}
			got := problems(t, raw)
			if !hasProblem(got, "not one of the state files") {
				t.Fatalf("problems %q", got)
			}
		})
	}
}

func TestReadRefusesRawHeaders(t *testing.T) {
	base := func(extra ...[]byte) []byte {
		parts := slices.Concat([][]byte{
			rawEntry("team.json", '0', 0o600, "{}"),
			rawBlock("secrets", '5', 0o700, 0),
		}, extra)
		return rawTar(parts...)
	}
	cases := map[string]struct {
		raw  []byte
		want string
	}{
		"file with a trailing slash":      {base(rawEntry("secrets/x/", '0', 0o600, "x")), "not one of the state files"},
		"team.json with a trailing slash": {base(rawEntry("team.json/", '0', 0o600, "x")), "not one of the state files"},
		"file named secrets slash":        {base(rawEntry("secrets/", '0', 0o600, "x")), "not one of the state files"},
		"pax sparse major":                {base(rawPax(map[string]string{"GNU.sparse.major": "1", "GNU.sparse.minor": "0"}, rawEntry("secrets/s", '0', 0o600, "x"))), ""},
		"pax sparse name":                 {base(rawPax(map[string]string{"GNU.sparse.name": "secrets/real"}, rawEntry("secrets/s", '0', 0o600, "x"))), "sparse"},
		"pax path traversal":              {base(rawPax(map[string]string{"path": "../../etc/cron.d/evil"}, rawEntry("secrets/benign", '0', 0o600, "x"))), "../../etc/cron.d/evil"},
		"pax path absolute":               {base(rawPax(map[string]string{"path": "/etc/passwd"}, rawEntry("secrets/benign", '0', 0o600, "x"))), "/etc/passwd"},
		"pax path to a bare name":         {base(rawPax(map[string]string{"path": "evil"}, rawEntry("secrets/benign", '0', 0o600, "x"))), "'evil'"},
		"pax linkpath on a symlink":       {base(rawPax(map[string]string{"linkpath": "/etc/shadow"}, rawBlock("secrets/l", '2', 0o777, 0))), "not one of the state files"},
		"pax mode setuid via record":      {base(rawEntry("secrets/m", '0', 0o4755, "x")), "setuid"},
		"gnu sparse type":                 {base(rawEntry("secrets/g", 'S', 0o600, "x")), ""},
		"gnu long name type":              {base(rawEntry("././@LongLink", 'L', 0o644, "secrets/../../etc/x\x00"), rawEntry("secrets/benign", '0', 0o600, "x")), "etc/x"},
		"gnu long link type":              {base(rawEntry("././@LongLink", 'K', 0o644, "/etc/shadow\x00"), rawBlock("secrets/l", '2', 0o777, 0)), "not one of the state files"},
		"vendor type":                     {base(rawEntry("secrets/v", 'V', 0o600, "x")), "not one of the state files"},
		"unknown type":                    {base(rawEntry("secrets/q", 'Q', 0o600, "x")), "not one of the state files"},
		"digit type 7":                    {base(rawEntry("secrets/7", '7', 0o600, "x")), "not one of the state files"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := problems(t, tc.raw)
			if tc.want != "" && !hasProblem(got, tc.want) {
				t.Fatalf("problems %q lack %q", got, tc.want)
			}
		})
	}
}

func TestReadRefusesSetuidSetgidSticky(t *testing.T) {
	for _, mode := range []int64{0o4755, 0o2755, 0o1755, 0o6755, 0o7777, 0o4000, 0o1000, 0o2000} {
		t.Run(strconv.FormatInt(mode, 8), func(t *testing.T) {
			f := file("secrets/x", "k")
			f.hdr.Mode = mode
			got := problems(t, build(t, file("team.json", "{}"), dir("secrets"), f))
			if !hasProblem(got, "carries a setuid, setgid or sticky bit") || !hasProblem(got, "mode "+strconv.FormatInt(mode, 8)) {
				t.Fatalf("problems %q", got)
			}
		})
	}
	t.Run("on a directory", func(t *testing.T) {
		d := dir("secrets")
		d.hdr.Mode = 0o1777
		if got := problems(t, build(t, file("team.json", "{}"), d)); !hasProblem(got, "sticky") {
			t.Fatalf("problems %q", got)
		}
	})
	t.Run("on team.json", func(t *testing.T) {
		f := file("team.json", "{}")
		f.hdr.Mode = 0o4600
		if got := problems(t, build(t, f, dir("secrets"))); !hasProblem(got, "setuid") {
			t.Fatalf("problems %q", got)
		}
	})
}

func TestReadRequiresTeamJSONAndSecrets(t *testing.T) {
	cases := map[string]struct {
		members []member
		missing []string
	}{
		"no secrets":  {[]member{file("team.json", "{}")}, []string{"secrets is missing"}},
		"no team":     {[]member{dir("secrets")}, []string{"team.json is missing"}},
		"nothing":     {nil, []string{"team.json is missing", "secrets is missing"}},
		"only others": {[]member{file("registry.json", "{}"), file("room_id", "x")}, []string{"team.json is missing", "secrets is missing"}},
		"secrets file only (no secrets dir member)": {
			[]member{file("team.json", "{}"), file("secrets/a", "k")},
			[]string{"secrets is missing"},
		},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got := problems(t, build(t, tc.members...))
			for _, want := range tc.missing {
				if !hasProblem(got, want) {
					t.Fatalf("problems %q lack %q", got, want)
				}
			}
		})
	}
}

// All problems are listed, not just the first, one per line.
func TestReadListsEveryProblem(t *testing.T) {
	f := file("secrets/x", "k")
	f.hdr.Mode = 0o4755
	raw := build(t, file("etc/passwd", "x"), symlink("secrets/y", "z"), f)
	err := func() error { _, e := Read(bytes.NewReader(raw), DefaultLimits()); return e }()
	var refused *RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("error %v", err)
	}
	got := refused.Problems
	for _, want := range []string{"'etc/passwd': not one of the state files", "'secrets/y': not one of the state files", "'secrets/x': mode 4755 carries", "team.json is missing", "secrets is missing"} {
		if !hasProblem(got, want) {
			t.Fatalf("problems %q lack %q", got, want)
		}
	}
	text := refused.Error()
	if strings.Count(text, "\n") != len(got)-1 {
		t.Fatalf("Error() is not one problem per line: %q", text)
	}
}

// A hostile member name must not forge extra lines or terminal escapes in the report.
func TestProblemLinesNeverCarryRawControlBytes(t *testing.T) {
	raw := build(t, file("team.json", "{}"), dir("secrets"), file("secrets/a\nsecrets/ok\x1b[2J\r", "x"))
	for _, p := range problems(t, raw) {
		for _, r := range p {
			if r < 0x20 || r == 0x7f {
				t.Fatalf("problem %q contains control byte %#x", p, r)
			}
		}
	}
}

func TestReadRefusesDuplicatesAndConflicts(t *testing.T) {
	cases := map[string][]member{
		"same file twice":           {file("secrets/a", "1"), file("secrets/a", "2")},
		"team.json twice":           {file("team.json", "{}")},
		"secrets twice":             {dir("secrets")},
		"file then dir same name":   {file("secrets/a", "1"), dir("secrets/a")},
		"dir then file same name":   {dir("secrets/a"), file("secrets/a", "1")},
		"file as parent of a file":  {file("secrets/a", "1"), file("secrets/a/b", "2")},
		"file as parent, other way": {file("secrets/a/b", "2"), file("secrets/a", "1")},
	}
	for name, extra := range cases {
		t.Run(name, func(t *testing.T) {
			members := append([]member{file("team.json", "{}"), dir("secrets")}, extra...)
			if got := problems(t, build(t, members...)); !hasProblem(got, "duplicate") && !hasProblem(got, "conflicts") {
				t.Fatalf("problems %q", got)
			}
		})
	}
}

// patchSize rewrites the size field and checksum of the first header named name.
func patchSize(t *testing.T, raw []byte, name string, size int) []byte {
	t.Helper()
	out := append([]byte(nil), raw...)
	for off := 0; off+512 <= len(out); off += 512 {
		block := out[off : off+512]
		if strings.TrimRight(string(block[:100]), "\x00") != name {
			continue
		}
		copy(block[124:136], []byte(strconv.FormatInt(int64(size), 8)))
		for i := 124 + len(strconv.FormatInt(int64(size), 8)); i < 135; i++ {
			block[i] = '0'
		}
		for i := 124; i < 135; i++ {
			if block[i] == 0 {
				block[i] = '0'
			}
		}
		block[135] = 0
		for i := 148; i < 156; i++ {
			block[i] = ' '
		}
		sum := 0
		for _, b := range block {
			sum += int(b)
		}
		copy(block[148:156], []byte(leftPad(strconv.FormatInt(int64(sum), 8), 6)+"\x00 "))
		return out
	}
	t.Fatalf("no header named %q", name)
	return nil
}

func leftPad(s string, n int) string {
	return strings.Repeat("0", n-len(s)) + s
}

func TestReadEnforcesLimits(t *testing.T) {
	t.Run("file over the per-file limit", func(t *testing.T) {
		lim := DefaultLimits()
		lim.MaxFileSize = 1024
		raw := build(t, file("team.json", "{}"), dir("secrets"), file("secrets/big", strings.Repeat("x", 1025)))
		_, err := Read(bytes.NewReader(raw), lim)
		var refused *RefusedError
		if !errors.As(err, &refused) || !hasProblem(refused.Problems, "over the") {
			t.Fatalf("error %v", err)
		}
	})
	t.Run("file at the per-file limit", func(t *testing.T) {
		lim := DefaultLimits()
		lim.MaxFileSize = 1024
		raw := build(t, file("team.json", "{}"), dir("secrets"), file("secrets/ok", strings.Repeat("x", 1024)))
		if _, err := Read(bytes.NewReader(raw), lim); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("zero limits are the defaults", func(t *testing.T) {
		if _, err := Read(bytes.NewReader(build(t, good()...)), Limits{}); err != nil {
			t.Fatal(err)
		}
	})
}

func required() []member {
	return []member{file("team.json", "{}"), dir("secrets")}
}

func TestReadEnforcesTheTotalAndTheCount(t *testing.T) {
	t.Run("total over the limit", func(t *testing.T) {
		lim := DefaultLimits()
		lim.MaxFileSize = 1024
		lim.MaxTotal = 2048
		members := required()
		for i := range 4 {
			members = append(members, file("secrets/f"+strconv.Itoa(i), strings.Repeat("x", 1000)))
		}
		_, err := Read(bytes.NewReader(build(t, members...)), lim)
		var refused *RefusedError
		if !errors.As(err, &refused) || !hasProblem(refused.Problems, "total") {
			t.Fatalf("error %v", err)
		}
	})
	t.Run("too many entries", func(t *testing.T) {
		lim := DefaultLimits()
		lim.MaxEntries = 10
		members := required()
		for i := range 20 {
			members = append(members, file("secrets/f"+strconv.Itoa(i), "x"))
		}
		_, err := Read(bytes.NewReader(build(t, members...)), lim)
		var refused *RefusedError
		if !errors.As(err, &refused) || !hasProblem(refused.Problems, "more than 10 members") {
			t.Fatalf("error %v", err)
		}
	})
}

func TestReadEnforcesTheNameAndDepthLimits(t *testing.T) {
	t.Run("name too long", func(t *testing.T) {
		lim := DefaultLimits()
		lim.MaxNameLen = 40
		raw := build(t, file("team.json", "{}"), dir("secrets"), file("secrets/"+strings.Repeat("n", 40), "x"))
		if got := problems2(t, raw, lim); !hasProblem(got, "name is longer than 40") {
			t.Fatalf("problems %q", got)
		}
	})
	t.Run("too deep", func(t *testing.T) {
		lim := DefaultLimits()
		lim.MaxDepth = 3
		raw := build(t, file("team.json", "{}"), dir("secrets"), file("secrets/a/b/c/d", "x"))
		if got := problems2(t, raw, lim); !hasProblem(got, "deeper than 3") {
			t.Fatalf("problems %q", got)
		}
	})
}

func problems2(t *testing.T, raw []byte, lim Limits) []string {
	t.Helper()
	_, err := Read(bytes.NewReader(raw), lim)
	var refused *RefusedError
	if !errors.As(err, &refused) {
		t.Fatalf("error %v", err)
	}
	return refused.Problems
}

// A declared size far beyond the data is a truncated tar, refused without reading the
// claimed size into memory.
func TestReadRefusesAHugeDeclaredSize(t *testing.T) {
	raw := build(t, file("team.json", "{}"), dir("secrets"), file("secrets/x", "tiny"))
	raw = patchSize(t, raw, "secrets/x", 0o7777777777)
	_, err := Read(bytes.NewReader(raw), DefaultLimits())
	if !errors.Is(err, ErrRefused) {
		t.Fatalf("error %v", err)
	}
}

func TestReadRefusesCorruptInput(t *testing.T) {
	valid := build(t, good()...)
	cases := map[string][]byte{
		"truncated in a header": valid[:700],
		"truncated in data":     valid[:512+3],
		"garbage":               bytes.Repeat([]byte("x"), 2048),
		"one flipped checksum":  flip(valid, 148),
		"gzip of a good tar":    gz(t, valid),
		"text":                  []byte("this is not a tar archive\n"),
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Read(bytes.NewReader(raw), DefaultLimits())
			if !errors.Is(err, ErrRefused) {
				t.Fatalf("error %v", err)
			}
		})
	}
}

func TestReadEmptyInputIsMissingTheStateFiles(t *testing.T) {
	got := problems(t, nil)
	if !hasProblem(got, "team.json is missing") || !hasProblem(got, "secrets is missing") {
		t.Fatalf("problems %q", got)
	}
}

func flip(raw []byte, at int) []byte {
	out := append([]byte(nil), raw...)
	out[at] ^= 0x01
	return out
}

func gz(t *testing.T, raw []byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(raw); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// Bytes after the end-of-archive marker are not read, so a second archive appended to the
// first contributes nothing: it can neither pass validation nor be extracted.
func TestReadIgnoresBytesAfterTheEndMarker(t *testing.T) {
	first := build(t, good()...)
	second := build(t, file("etc/passwd", "evil"))
	s, err := Read(bytes.NewReader(append(append([]byte(nil), first...), second...)), DefaultLimits())
	if err != nil {
		t.Fatal(err)
	}
	if s.Has("etc/passwd") {
		t.Fatal("a member after the end marker was read")
	}
}

// One parser: a pax "path" record replaces the name in the ustar header. Reading names the
// member by the path it will be extracted to, so the traversal is seen and refused; a second
// parser that read only the ustar name would pass it.
func TestReadSeesThePaxPathNotTheUstarName(t *testing.T) {
	raw := rawTar(
		rawPax(map[string]string{"path": "../../etc/cron.d/evil"}, rawEntry("team.json", '0', 0o600, "{}")),
		rawBlock("secrets", '5', 0o700, 0),
	)
	got := problems(t, raw)
	if !hasProblem(got, "../../etc/cron.d/evil") {
		t.Fatalf("problems %q do not name the pax path", got)
	}
}

// A GNU long-name member carries the real name; it is the name that is judged.
func TestReadSeesTheGNULongName(t *testing.T) {
	long := "secrets/" + strings.Repeat("d/", 60) + "../../../../../../../../../../../../etc/passwd"
	m := file(long, "x")
	m.hdr.Format = tar.FormatGNU
	raw := build(t, file("team.json", "{}"), dir("secrets"), m)
	if got := problems(t, raw); !hasProblem(got, "not one of the state files") {
		t.Fatalf("problems %q", got)
	}
}

func TestRefusedErrorShape(t *testing.T) {
	e := &RefusedError{Problems: []string{"a", "b"}}
	if e.Error() != "a\nb" {
		t.Fatalf("Error() = %q", e.Error())
	}
	if !errors.Is(e, ErrRefused) {
		t.Fatal("not ErrRefused")
	}
}
