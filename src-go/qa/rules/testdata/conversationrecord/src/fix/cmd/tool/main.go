// Package main stands for a command, judged as the packages are.
package main

import "os"

func main() {
	if _, err := os.ReadFile("/s/conversation.json"); err != nil { // want `conversation record's path named outside conversation.Locate`
		os.Exit(1)
	}
}
