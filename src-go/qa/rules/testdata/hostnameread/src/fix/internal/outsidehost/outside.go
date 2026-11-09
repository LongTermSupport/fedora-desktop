// Package outsidehost is outside teamchat: the rule does not judge it.
package outsidehost

import "os"

// Name reads the machine's hostname, which is nothing to teamchat.
func Name() (string, error) { return os.Hostname() }
