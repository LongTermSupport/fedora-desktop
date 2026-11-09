package badhost

import osalias "os"

// Aliased reads the hostname through an import under another name.
func Aliased() (string, error) {
	return osalias.Hostname() // want `os.Hostname read outside teamchat/declare/machinehost.go`
}
