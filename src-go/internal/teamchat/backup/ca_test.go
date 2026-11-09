package backup

import (
	"archive/tar"
	"bytes"
	"context"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

// wallAt is a pki clock stopped at a millisecond.
type wallAt int64

func (w wallAt) WallMS() int64 { return int64(w) }

// newCA is a team CA made on a fixed clock.
func newCA(t *testing.T) pki.CA {
	t.Helper()
	ca, err := pki.Maker{Clock: wallAt(sampleTimeMS)}.NewCA("teamchat alpha CA")
	if err != nil {
		t.Fatal(err)
	}
	return ca
}

// caFiles are the files of ca a backup carries, as host.CABackupFiles names them.
func caFiles(ca pki.CA) []host.BackupFile {
	return []host.BackupFile{
		{Name: "ca.crt", Mode: 0o644, Data: ca.CertPEM()},
		{Name: "ca.key", Mode: 0o600, Data: ca.KeyPEM()},
	}
}

// caTar is backup n of sampleSources with ca's files.
func caTar(t *testing.T, ca pki.CA, n int64) []byte {
	t.Helper()
	s := sampleSources(t)
	s.CA = caFiles(ca)
	raw, err := Encode(sampleManifest(t, n), s)
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

// sameCA holds got to exactly ca's two files, in order, with their modes.
func sameCA(t *testing.T, got []host.BackupFile, ca pki.CA) {
	t.Helper()
	want := caFiles(ca)
	if len(got) != len(want) {
		t.Fatalf("the backup carries %d CA files, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i].Name != want[i].Name || got[i].Mode != want[i].Mode || !bytes.Equal(got[i].Data, want[i].Data) {
			t.Errorf("CA file %d is %s mode %o; want %s mode %o with the CA's bytes", i, got[i].Name, got[i].Mode, want[i].Name, want[i].Mode)
		}
	}
}

// Plan 00246 X13: a backup of a team with a CA is version 2 of the manifest, and carries the CA's
// certificate (0644) and key (0600) after the six members of version 1; it reads back as written.
func TestEncodeCarriesTheCA(t *testing.T) {
	ca := newCA(t)
	raw := caTar(t, ca, 3)
	r := tar.NewReader(bytes.NewReader(raw))
	var names []string
	for {
		h, err := r.Next()
		if err != nil {
			break
		}
		names = append(names, h.Name)
		want := int64(0o600)
		if h.Name == "ca.crt" {
			want = 0o644
		}
		if h.Typeflag != tar.TypeReg || h.Mode != want || h.Uid != 0 || h.Gid != 0 {
			t.Fatalf("member %s: %+v", h.Name, h)
		}
	}
	if got := strings.Join(names, " "); got != "backup.json team.json config.toml roster.json rooms.json admin.token ca.crt ca.key" {
		t.Fatalf("members %s", got)
	}
	if m := string(memberData(t, raw, "backup.json")); !strings.HasPrefix(m, `{"v":2,`) {
		t.Fatalf("manifest %s, want version 2", m)
	}
	c, err := Decode(raw, alpha(t), 3)
	if err != nil {
		t.Fatal(err)
	}
	sameCA(t, c.CA, ca)
}

// X13: a backup taken before the CA was carried (version 1, six members) still reads, holding no
// CA, and a team with no CA yet is backed up as version 1.
func TestABackupWithoutTheCAHoldsNone(t *testing.T) {
	c, err := Decode(sampleTar(t, 3), alpha(t), 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(c.CA) != 0 {
		t.Fatalf("a version 1 backup holds %d CA files", len(c.CA))
	}
}

// X13: Encode carries exactly the CA's two files, in order, with their modes, or refuses.
func TestEncodeRefusesABadCA(t *testing.T) {
	ca := newCA(t)
	for name, files := range map[string][]host.BackupFile{
		"the certificate alone": caFiles(ca)[:1],
		"the key alone":         caFiles(ca)[1:],
		"out of order":          {caFiles(ca)[1], caFiles(ca)[0]},
		"a key mode 0644":       {caFiles(ca)[0], {Name: "ca.key", Mode: 0o644, Data: ca.KeyPEM()}},
		"another name":          {caFiles(ca)[0], {Name: "leaf.key", Mode: 0o600, Data: ca.KeyPEM()}},
		"a third file":          append(caFiles(ca), caFiles(ca)[1]),
		"a certificate over its cap": {
			{Name: "ca.crt", Mode: 0o644, Data: bytes.Repeat([]byte("a"), capSmall+1)}, caFiles(ca)[1],
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := sampleSources(t)
			s.CA = files
			if _, err := Encode(sampleManifest(t, 1), s); err == nil {
				t.Fatal("encoded")
			}
		})
	}
}

// X13 and BK3: the manifest's version says how many members the tar holds; a CA member with
// another mode, a CA half there and a version no backup has are refused.
func TestDecodeRefusesABadCATar(t *testing.T) {
	ca := newCA(t)
	withCA := func(t *testing.T) []member {
		t.Helper()
		var out []member
		r := tar.NewReader(bytes.NewReader(caTar(t, ca, 3)))
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
	manifest := func(v string) func([]member) []member {
		return func(ms []member) []member {
			ms[0].data = []byte(`{"v":` + v + `,"team":"alpha","backup":3,"time_ms":1792663930000,"server_name":"alpha.teamchat.internal"}`)
			ms[0].h.Size = int64(len(ms[0].data))
			return ms
		}
	}
	for name, mutate := range map[string]func([]member) []member{
		"version 1 with the CA":   manifest("1"),
		"version 3":               manifest("3"),
		"the certificate alone":   func(ms []member) []member { return ms[:7] },
		"the CA twice":            func(ms []member) []member { return append(ms, ms[7]) },
		"a certificate mode 0600": func(ms []member) []member { ms[6].h.Mode = 0o600; return ms },
		"a key mode 0644":         func(ms []member) []member { ms[7].h.Mode = 0o644; return ms },
		"the CA files swapped":    func(ms []member) []member { ms[6], ms[7] = ms[7], ms[6]; return ms },
		"a key that is not the CA's": func(ms []member) []member {
			ms[7].data = newCA(t).KeyPEM()
			ms[7].h.Size = int64(len(ms[7].data))
			return ms
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := Decode(writeTar(t, mutate(withCA(t))), alpha(t), 3); err == nil {
				t.Fatal("decoded")
			}
		})
	}
	if _, err := Decode(writeTar(t, withCA(t)), alpha(t), 3); err != nil {
		t.Fatalf("the unchanged tar: %v", err)
	}
}

// plantCA writes ca where the host keeps a team's CA, with the host's modes.
func (w *world) plantCA(ca pki.CA) {
	w.t.Helper()
	if err := os.MkdirAll(w.root+paths.CADir(w.team), 0o700); err != nil {
		w.t.Fatal(err)
	}
	for _, f := range caFiles(ca) {
		if err := os.WriteFile(w.root+paths.CADir(w.team)+"/"+f.Name, f.Data, f.Mode); err != nil {
			w.t.Fatal(err)
		}
	}
}

// X13, owner answer 4: a backup Take makes carries the team's CA, and restoring it into a host
// with no CA gives the team the same CA, so every bundle issued before stays valid.
func TestTakeCarriesTheTeamCA(t *testing.T) {
	w := newWorld(t)
	ca := newCA(t)
	w.plantCA(ca)
	res, err := w.take()
	if err != nil {
		t.Fatal(err)
	}
	c, err := ReadTar(w.deps(), w.team, res.Backup)
	if err != nil {
		t.Fatal(err)
	}
	sameCA(t, c.CA, ca)
	clean := t.TempDir()
	// A restore runs on an applied team, so its root state directory is there and its CA is not.
	if err = os.MkdirAll(clean+paths.AdminState(w.team), 0o700); err != nil {
		t.Fatal(err)
	}
	written, err := host.RestoreCA(clean, w.team, selfUID(t), c.CA)
	if err != nil || !written {
		t.Fatalf("the restore into a clean root: %v, %v", written, err)
	}
	pem, err := host.CAPEM(clean, w.team, selfUID(t))
	if err != nil {
		t.Fatal(err)
	}
	before, err := pki.Fingerprint(ca.CertPEM())
	if err != nil {
		t.Fatal(err)
	}
	after, err := pki.Fingerprint([]byte(pem))
	if err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatalf("the restored CA is %s, want %s", after, before)
	}
}

// X13: a team with no CA yet is backed up without one; a CA the host could not use fails the
// backup at its sources step, naming the file, since a backup of it could not be restored.
func TestTakeOfNoCAOrABadCA(t *testing.T) {
	w := newWorld(t)
	res, err := w.take()
	if err != nil {
		t.Fatal(err)
	}
	c, err := ReadTar(w.deps(), w.team, res.Backup)
	if err != nil || len(c.CA) != 0 {
		t.Fatalf("a team with no CA: %d CA files, %v", len(c.CA), err)
	}
	w = newWorld(t)
	w.plantCA(newCA(t))
	if err = os.WriteFile(w.root+paths.CAKeyFile(w.team), []byte("junk"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err = Take(context.Background(), w.deps(), w.lock, nil)
	if err == nil || !strings.Contains(err.Error(), StepSources) || !strings.Contains(err.Error(), "ca.key") {
		t.Fatalf("a bad CA key: %v, want a failure at the sources step naming ca.key", err)
	}
	if got := w.tars(); !slices.Equal(got, []string{"last-run.json"}) {
		t.Fatalf("a failed backup left %v", got)
	}
}
