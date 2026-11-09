// Package other has a file of the exempt file's name, in another package: the exemption is the
// whole path, so it is judged.
package other

import "os"

// MachineHostname reads the hostname where the exemption does not reach.
func MachineHostname() (string, error) {
	return os.Hostname() // want `os.Hostname read outside teamchat/declare/machinehost.go`
}
