package declare

import "os"

// MachineHostname is the machine's hostname, the real side of the seam CheckSites takes. This
// file is the one place teamchat reads it (project_hostname_read exempts exactly this path):
// the hostname is compared with a declared site and refused, never used to make one
// (SPEC-cross-network X25 to X27).
func MachineHostname() (string, error) { return os.Hostname() }
