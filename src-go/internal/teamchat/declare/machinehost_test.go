package declare

import "testing"

// MachineHostname is the seam's real side: the machine's hostname, which a test replaces whole
// with a fake. It cannot be compared with another read of the hostname, since no test reads one
// (project_hostname_read).
func TestMachineHostnameIsTheMachinesName(t *testing.T) {
	name, err := MachineHostname()
	if err != nil || name == "" {
		t.Fatalf("hostname %q, %v; want a name", name, err)
	}
}
