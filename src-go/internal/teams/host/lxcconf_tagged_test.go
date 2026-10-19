//go:build ccyteamstest

package host

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/hostcheckout"
)

func TestTheTestTagReadsTheStandInLxcConf(t *testing.T) {
	stand := filepath.Join(t.TempDir(), "lxc.conf")
	if err := os.WriteFile(stand, []byte("lxc.lxcpath = /srv/lxc\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	read := lxcConfReader(func(k string) string { return map[string]string{testLxcConfEnv: stand}[k] })
	got, err := read(hostcheckout.LxcConf)
	if err != nil || string(got) != "lxc.lxcpath = /srv/lxc\n" {
		t.Fatalf("read %q, %v", got, err)
	}
}
