package link

import "fix/internal/teamchat/paths"

// Asked takes the socket from the resolver; a comment naming cc-socks is not code.
func Asked(pid int) string { return paths.InboxSocket(pid) }

// Other names a directory that is not the inbox sockets'.
func Other(dir string) string { return dir + "/cc-sockets-not/x.sock" }
