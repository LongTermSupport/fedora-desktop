// Package main stands for the teamchat command, which the rule judges as it does the packages.
package main

import "os"

func main() {
	if _, err := os.Hostname(); err != nil { // want `os.Hostname read outside teamchat/declare/machinehost.go`
		os.Exit(1)
	}
}
