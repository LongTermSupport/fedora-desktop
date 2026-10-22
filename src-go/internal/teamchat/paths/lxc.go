package paths

import "regexp"

// The names an LXC member's host side adds (C 3.7.5, 3.9.3).

// ContainerName is an LXC container's name, in declare's grammar, so a path can take one.
type ContainerName struct{ s string }

// containerGrammar is one path segment that is never "." or "..", and never reads as an option.
var containerGrammar = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)

// ParseContainerName checks a container name.
func ParseContainerName(s string) (ContainerName, error) {
	v, err := check("container name", containerGrammar, s)
	return ContainerName{v}, err
}

// String is the container name's text.
func (c ContainerName) String() string { return c.s }

// MarshalText encodes the container name, and refuses the zero value.
func (c ContainerName) MarshalText() ([]byte, error) { return marshal("container name", c.s) }

// UnmarshalText decodes a container name through ParseContainerName.
func (c *ContainerName) UnmarshalText(b []byte) error {
	return unmarshal(c, ParseContainerName, string(b))
}

func (c ContainerName) text() string { return must("container name", c.s) }

// LXCDesiredCopy is the host's copy of a guest user's desired membership (C 3.9.3).
func LXCDesiredCopy(c ContainerName, u GuestUser) string {
	return AdminLXCDir + "/" + c.text() + "/" + u.text() + ".json"
}

// LXCSession is a guest user's session name, lxc-<user> (C 3.7.5); `guest membership` refuses
// any other (GU4).
func LXCSession(u GuestUser) SessionName { return SessionName{"lxc-" + u.text()} }
