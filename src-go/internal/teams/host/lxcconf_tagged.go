//go:build ccyteamstest

package host

import (
	"os"
	"path/filepath"
)

// testLxcConfEnv names a stand-in for root's LXC configuration.
const testLxcConfEnv = "CCY_TEAMS_TEST_LXC_CONF"

// lxcConfReader reads, in the binary the ccy-teams parity suite builds with -tags ccyteamstest,
// the stand-in testLxcConfEnv names instead of root's LXC configuration, so the suite can place
// a container's tree in its scratch folder. The production build never compiles this file.
func lxcConfReader(getenv func(string) string) func(string) ([]byte, error) {
	return func(path string) ([]byte, error) {
		if stand := getenv(testLxcConfEnv); stand != "" {
			path = stand
		}
		return os.ReadFile(filepath.Clean(path))
	}
}
