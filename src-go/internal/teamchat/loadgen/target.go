package loadgen

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// TestDomain is the name every target is under: `.test`, reserved for testing (RFC 6761
// section 6.2) and never delegated, so no real homeserver has it. A team's own server is
// `<team>.teamchat.internal`.
const TestDomain = ".test"

// labelForm is one DNS label in lower case.
var labelForm = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// underTest is whether name is a lower-case DNS name of two labels or more ending in `.test`.
func underTest(name string) bool {
	rest, ok := strings.CutSuffix(name, TestDomain)
	if !ok || rest == "" || len(name) > 253 {
		return false
	}
	for label := range strings.SplitSeq(rest, ".") {
		if !labelForm.MatchString(label) {
			return false
		}
	}
	return true
}

// CheckTarget is LG1's rule for the homeserver a run may touch: the server name under
// TestDomain, and the URL plain http to a loopback IP literal (the matrix client's rule for
// plain http), with no user, path, query or fragment.
func CheckTarget(base, serverName string) error {
	switch {
	case base == "":
		return errors.New("no --homeserver: name the lab homeserver's URL")
	case serverName == "":
		return errors.New("no --server-name: name the lab homeserver's server name")
	case !underTest(serverName):
		return fmt.Errorf("the server name %q is not under %s: the load generator drives only a lab server named under %s (RFC 6761), never a team's", serverName, TestDomain, TestDomain)
	}
	u, err := url.Parse(base)
	if err != nil {
		return fmt.Errorf("--homeserver %q is not a URL: %w", base, err)
	}
	if u.Scheme != "http" {
		return fmt.Errorf("--homeserver %q is not plain http: the lab server listens on loopback, where the matrix client takes plain http only", base)
	}
	if u.User != nil || strings.Trim(u.Path, "/") != "" || u.RawQuery != "" || u.Fragment != "" || u.Opaque != "" {
		return fmt.Errorf("--homeserver %q has a user, path, query or fragment: give the server's root URL only", base)
	}
	if ip := net.ParseIP(u.Hostname()); ip != nil && ip.IsLoopback() {
		return nil
	}
	return fmt.Errorf("--homeserver %q is not a loopback IP (127.0.0.0/8 or ::1): the load generator drives only a lab server on this machine", base)
}

// CheckServer is LG1's check of the server's own answer: the account it registered is on the
// server name the run was given, so a server with another name stops the run before it writes
// anything more.
func CheckServer(userID, serverName string) error {
	if _, ok := proto.Localpart(userID, serverName); !ok {
		return fmt.Errorf("the server registered %q, which is not on %s: it is another server than the one named", userID, serverName)
	}
	return nil
}
