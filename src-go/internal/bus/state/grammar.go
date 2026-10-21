package state

import (
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// The protocol's grammars this package checks, as helpers/pingbus/protocol.py spells them.
// TODO(bus/proto): every name in this file is bus/proto's; replace each with its export there.
const (
	dnsLabel       = `[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?`
	descriptionMax = 120
	headlineMax    = 120
	reasonMax      = 200
	userIDTextMax  = 255
)

var (
	teamPattern      = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)
	seatPattern      = regexp.MustCompile(`^(?:[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11})$`)
	handlePattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,47}\.(?:[1-9][0-9]{0,5}|[a-z][a-z0-9]{0,11})\+` + dnsLabel + `\.(podman|lxc|docker|vm|host)$`)
	humanPattern     = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	roomIDPattern    = regexp.MustCompile(`^![A-Za-z0-9_-]{43}$`)
	eventIDPattern   = regexp.MustCompile(`^\$[A-Za-z0-9_-]{43}$`)
	printablePattern = regexp.MustCompile(`^[\x21-\x7e]+$`)

	reservedLocalparts = []string{"admin", "conduit"}
	// ackVerbs are the verbs whose rule expects an ack (protocol.VERBS, ack_expected).
	ackVerbs    = []string{"fetch", "sync", "review", "run-qa", "halt"}
	dropReasons = []string{
		"version", "schema", "size", "edit", "sender", "role", "target", "verb", "ref",
		"allowlist", "excluded", "re", "body", "stale", "rate", "forge-rate", "unresolved", "provenance",
	}
	unheardReasons = []string{"not-addressed"}
)

func isTeamName(s string) bool       { return teamPattern.MatchString(s) }
func isSeatName(s string) bool       { return seatPattern.MatchString(s) }
func isHandle(s string) bool         { return handlePattern.MatchString(s) }
func isRoomID(s string) bool         { return roomIDPattern.MatchString(s) }
func isEventID(s string) bool        { return eventIDPattern.MatchString(s) }
func isPrintableToken(s string) bool { return printablePattern.MatchString(s) }
func isAckVerb(s string) bool        { return slices.Contains(ackVerbs, s) }
func isDropReason(s string) bool     { return slices.Contains(dropReasons, s) }
func isUnheardReason(s string) bool  { return slices.Contains(unheardReasons, s) }

func isHumanLocalpart(s string) bool {
	return humanPattern.MatchString(s) && !slices.Contains(reservedLocalparts, s)
}

// isUserIDText is inbox.is_user_id_text: `@` and up to 254 printable ASCII characters with no
// space, a `:` after the second character.
func isUserIDText(s string) bool {
	return len(s) >= 2 && len(s) <= userIDTextMax && s[0] == '@' && isPrintableToken(s[1:]) &&
		strings.Contains(s[2:], ":")
}

// isReason is inbox._is_reason: 1 to 200 printable ASCII characters, spaces included.
func isReason(s string) bool {
	if s == "" || len(s) > reasonMax {
		return false
	}
	for _, c := range []byte(s) {
		if c < ' ' || c > '~' {
			return false
		}
	}
	return true
}

// isDescription is protocol.is_description: 1 to 120 code points, already stripped, no Unicode
// control or format character.
func isDescription(s string) bool {
	runes, ok := pyjson.CodePoints(s)
	if !ok || len(runes) == 0 || len(runes) > descriptionMax {
		return false
	}
	if unicode.IsSpace(runes[0]) || unicode.IsSpace(runes[len(runes)-1]) {
		return false
	}
	for _, r := range runes {
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) {
			return false
		}
	}
	return true
}

// parseUserID is protocol.parse_user_id: the localpart of `@<localpart>:<server>` when it is a
// handle, a reserved localpart or a human localpart, on exactly this server.
func parseUserID(s, server string) (string, bool) {
	rest, ok := strings.CutPrefix(s, "@")
	if !ok {
		return "", false
	}
	localpart, on, found := strings.Cut(rest, ":")
	if !found || on != server {
		return "", false
	}
	if isHandle(localpart) || slices.Contains(reservedLocalparts, localpart) || isHumanLocalpart(localpart) {
		return localpart, true
	}
	return "", false
}

// isAgentUserID is protocol.is_agent_user_id.
func isAgentUserID(s, server string) bool {
	localpart, ok := parseUserID(s, server)
	return ok && isHandle(localpart)
}
