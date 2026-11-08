package restore

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/host"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
)

// caWall is a pki clock stopped at the backup's time.
type caWall struct{}

func (caWall) WallMS() int64 { return backupMS }

// caFilesOf is a new CA's files as a backup carries them.
func caFilesOf(t *testing.T) []host.BackupFile {
	t.Helper()
	ca, err := pki.Maker{Clock: caWall{}}.NewCA("teamchat ops CA")
	if err != nil {
		t.Fatal(err)
	}
	return []host.BackupFile{{Name: "ca.crt", Mode: 0o644, Data: ca.CertPEM()}, {Name: "ca.key", Mode: 0o600, Data: ca.KeyPEM()}}
}

func fingerprint(t *testing.T, pem []byte) string {
	t.Helper()
	fp, err := pki.Fingerprint(pem)
	if err != nil {
		t.Fatal(err)
	}
	return fp
}

// teamCA is the fingerprint of the CA the world's host keeps, "" for none.
func (w *world) teamCA() string {
	w.t.Helper()
	if !w.has(paths.CACertFile(w.team)) {
		return ""
	}
	pem, err := host.CAPEM(w.os.Root, w.team, w.os.StateOwnerUID)
	if err != nil {
		w.t.Fatal(err)
	}
	return fingerprint(w.t, []byte(pem))
}

// plantCA gives the world's host a CA, as an apply would have made it.
func (w *world) plantCA(files []host.BackupFile) {
	w.t.Helper()
	if written, err := host.RestoreCA(w.os.Root, w.team, w.os.StateOwnerUID, files); err != nil || !written {
		w.t.Fatalf("planting the CA: %v, %v", written, err)
	}
}

// Plan 00246 X13, owner answer 4: a restore into a team with no CA puts the backup's CA back, so
// every bundle it issued stays valid, and says that the next apply makes the leaf from it.
func TestRunRestoresTheBackupsCA(t *testing.T) {
	w := newWorld(t)
	w.tarCA = caFilesOf(t)
	a, err := run(w, w.request())
	if err != nil {
		t.Fatal(err)
	}
	if got, want := w.teamCA(), fingerprint(t, w.tarCA[0].Data); got != want {
		t.Fatalf("the team's CA after the restore is %q, want the backup's %q", got, want)
	}
	if !slices.Contains(a.Changed, "CHANGED ca restored from backup 7") {
		t.Errorf("changed = %q", a.Changed)
	}
	if !slices.ContainsFunc(a.Notes, func(n string) bool { return strings.Contains(n, "run team apply") }) {
		t.Errorf("notes = %q, want one saying to run team apply", a.Notes)
	}
}

// X13: a backup that holds another CA than the team's is refused at validation, before anything
// is staged, and the team's CA is left as it is.
func TestRunRefusesABackupOfAnotherCA(t *testing.T) {
	w := newWorld(t)
	mine := caFilesOf(t)
	w.plantCA(mine)
	w.tarCA = caFilesOf(t)
	_, err := run(w, w.request())
	if !errors.Is(err, ErrRefused) || !strings.Contains(err.Error(), "another CA") {
		t.Fatalf("got %v, want a refusal naming another CA", err)
	}
	if w.has(paths.RestoreRecord(w.team)) || w.has(paths.RestoreConfig(w.team)) {
		t.Error("a refused restore recorded or staged something")
	}
	if got := w.teamCA(); got != fingerprint(t, mine[0].Data) {
		t.Error("a refused restore changed the team's CA")
	}
}

// X13: a backup of the same CA restores and changes nothing of it; one taken before the backup
// carried the CA leaves the team's CA as it is, and the answer says so.
func TestRunKeepsTheTeamsCA(t *testing.T) {
	w := newWorld(t)
	mine := caFilesOf(t)
	w.plantCA(mine)
	w.tarCA = mine
	a, err := run(w, w.request())
	if err != nil {
		t.Fatal(err)
	}
	if slices.ContainsFunc(a.Changed, func(c string) bool { return strings.Contains(c, "ca restored") }) {
		t.Errorf("a restore of the team's own CA says it restored it: %q", a.Changed)
	}
	w = newWorld(t)
	w.plantCA(mine)
	if a, err = run(w, w.request()); err != nil {
		t.Fatal(err)
	}
	if got := w.teamCA(); got != fingerprint(t, mine[0].Data) {
		t.Error("a backup without a CA changed the team's CA")
	}
	if !slices.ContainsFunc(a.Notes, func(n string) bool { return strings.Contains(n, "holds no team CA") }) {
		t.Errorf("notes = %q, want one saying the backup holds no team CA", a.Notes)
	}
}
