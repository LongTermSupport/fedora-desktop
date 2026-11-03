package badhost

import (
	"os"
	"testing"
)

// A test is judged as the code is: a hostname reached in a test is as much a hostname as one
// reached in the code, and the seam exists so a test uses a fake.
func TestReadsTheHostname(t *testing.T) {
	name, err := os.Hostname() // want `os.Hostname read outside teamchat/declare/machinehost.go`
	if err != nil || name == "" {
		t.Skip("no hostname")
	}
}
