//go:build !ccyteamstest

package host

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/hostcheckout"
)

// The production build reads root's LXC configuration where LXC keeps it, whatever the
// environment says.
func TestTheUntaggedBuildReadsTheRealLxcConf(t *testing.T) {
	stand := filepath.Join(t.TempDir(), "lxc.conf")
	if err := os.WriteFile(stand, []byte("lxc.lxcpath = /srv/lxc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	read := lxcConfReader(func(k string) string { return map[string]string{testLxcConfEnv: stand}[k] })
	want, wantErr := os.ReadFile(hostcheckout.LxcConf)
	got, err := read(hostcheckout.LxcConf)
	if string(got) != string(want) || (err == nil) != (wantErr == nil) {
		t.Fatalf("read %q (%v), want the real file's %q (%v)", got, err, want, wantErr)
	}
}
