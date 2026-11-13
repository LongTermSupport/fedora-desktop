// Package paths holds the one resolver of the inbox socket directory: exempt.
package paths

import "strconv"

// InboxSocket is the resolver, which names the directory.
func InboxSocket(pid int) string { return "/tmp/cc-socks/" + strconv.Itoa(pid) + ".sock" }
