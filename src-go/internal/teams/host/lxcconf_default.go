//go:build !ccyteamstest

package host

import "os"

// testLxcConfEnv names the stand-in for root's LXC configuration that only the parity suite's
// build reads (lxcconf_tagged.go); this build ignores it.
const testLxcConfEnv = "CCY_TEAMS_TEST_LXC_CONF"

// lxcConfReader is how root's LXC configuration is read: the file itself, in every build but the
// parity suite's. No environment can point it elsewhere.
func lxcConfReader(func(string) string) func(string) ([]byte, error) { return os.ReadFile }
