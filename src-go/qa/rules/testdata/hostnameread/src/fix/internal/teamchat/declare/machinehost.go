// Package declare stands for teamchat's declaration reader. Its one file machinehost.go is the one
// place the rule lets the machine's hostname be read.
package declare

import "os"

// MachineHostname is the one read of the machine's hostname (the exempt file).
func MachineHostname() (string, error) { return os.Hostname() }
