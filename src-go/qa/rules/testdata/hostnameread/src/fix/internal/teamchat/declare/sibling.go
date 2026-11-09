package declare

import "os"

// Sibling is in the exempt file's package, which is not the exemption: the exemption is the file.
func Sibling() (string, error) {
	return os.Hostname() // want `os.Hostname read outside teamchat/declare/machinehost.go`
}
