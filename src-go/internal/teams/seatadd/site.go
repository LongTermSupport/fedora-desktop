package seatadd

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/declare/hostvars"
)

// HostVarsSite is the Site seam over the host_vars file at path: its teamchat_site, "" when it
// declares none. The file is read whole by the strict v2 reader, which reads only the v2 keys,
// so a malformed v2 declaration refuses the run rather than issue under a guessed site.
func HostVarsSite(path string) func() (string, error) {
	return func() (string, error) {
		data, err := os.ReadFile(filepath.Clean(path))
		if err != nil {
			return "", fmt.Errorf("read the host_vars: %w", err)
		}
		d, err := hostvars.Read(data)
		if err != nil {
			return "", fmt.Errorf("the host_vars %s: %w", path, err)
		}
		return d.Site, nil
	}
}
