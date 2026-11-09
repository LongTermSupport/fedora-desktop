// Package main stands for the teamchat-admin command, which the rule judges as it does the packages.
package main

import (
	"net"
	"os"
)

func main() {
	if _, err := net.LookupAddr("192.0.2.7"); err != nil { // want `net.LookupAddr read outside teamchat/declare/machinehost.go`
		os.Exit(1)
	}
}
