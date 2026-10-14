// Package hostnet owns the network tools and curl.
package hostnet

import "fix/internal/execx"

// Commands are the owner's own use of its programs: never flagged.
func Commands() []execx.Cmd {
	return []execx.Cmd{{Name: "ip"}, {Name: "ss"}, {Name: "nmcli"}, {Name: "curl"}}
}
