package lightning

import (
	"fmt"
	"regexp"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/seat"
)

// The clients a team may open in (element.py CLIENTS); an entry without one is an Element team.
const (
	clientElement   = "element"
	clientLightning = "lightning"
)

// entry is one validated team of the list.
type entry struct{ team, baseURL, client string }

// The list's grammar, element.py's and the pingbus rules it calls: protocol.TEAM_NAME_PATTERN,
// teamfile.DNS_LABEL_PATTERN and SERVER_NAME_TLD. A team with no server_name has the default,
// <team>.agent-bus.internal, which the rule always accepts, so it is not built here.
const (
	teamPattern   = `[a-z][a-z0-9-]{0,23}`
	serverNameTLD = "internal"
	serverNameMax = 253
)

var (
	teamName = regexp.MustCompile(`^` + teamPattern + `$`)
	dnsLabel = regexp.MustCompile(`^[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?$`)
)

// The entry's keys (element.py PROFILE_KEYS).
const (
	keyBaseURL    = "base_url"
	keyCAFile     = "ca_file"
	keyClient     = "client"
	keyServerName = "server_name"
	keyTeam       = "team"
)

// entryKeys are the keys in the order a refusal lists them.
var entryKeys = []string{keyBaseURL, keyCAFile, keyClient, keyServerName, keyTeam}

// parseEntries is element.py parse_profiles over the stdin text (L3).
func parseEntries(text string) ([]entry, error) {
	v, err := pycompat.Loads(text, pycompat.LoadOptions{NoDuplicateKeys: true})
	if err != nil {
		return nil, refuse("the team list: not valid JSON (%v)", err)
	}
	items, ok := v.(pycompat.List)
	if !ok {
		return nil, refuse("the team list: must be a JSON array")
	}
	entries := make([]entry, 0, len(items))
	for i, item := range items {
		e, err := parseEntry(i+1, item)
		if err != nil {
			return nil, err
		}
		if slices.ContainsFunc(entries, func(seen entry) bool { return seen.team == e.team }) {
			return nil, refuse("the team list: a team is listed more than once")
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// parseEntry is element.py _parse_one, in its order, up to the team's name.
func parseEntry(position int, v pycompat.Value) (entry, error) {
	d, ok := v.(*pycompat.Dict)
	if !ok {
		return entry{}, refuse("entry %d: must be an object", position)
	}
	for _, key := range d.Keys() {
		if !slices.Contains(entryKeys, key) {
			return entry{}, refuse("entry %d: the only keys are %s", position, strings.Join(entryKeys, ", "))
		}
	}
	for _, key := range []string{keyTeam, keyBaseURL} {
		if !has(d, key) {
			return entry{}, refuse("entry %d: %s is required", position, key)
		}
	}
	team, ok := text(d, keyTeam)
	if !ok || !teamName.MatchString(team) {
		return entry{}, refuse("entry %d: team must match %s", position, teamPattern)
	}
	return parseTeam(team, d)
}

// parseTeam is the rest of _parse_one, for an entry whose team is known: each refusal names it.
func parseTeam(team string, d *pycompat.Dict) (entry, error) {
	refuseKey := func(key, why string) error { return refuse("team %s: %s %s", team, key, why) }
	baseURL, isText := text(d, keyBaseURL)
	var hosts []string
	if isText {
		hosts = seat.IPHost(baseURL)
	}
	if err := seat.CheckBaseURL(value(d, keyBaseURL), hosts, refuseKey); err != nil {
		if isRefusal(err) {
			return entry{}, err
		}
		return entry{}, refuseKey(keyBaseURL, "is not a URL")
	}
	if has(d, keyServerName) {
		if why := checkServerName(value(d, keyServerName)); why != "" {
			return entry{}, refuseKey(keyServerName, why)
		}
	}
	if caFile := value(d, keyCAFile); caFile != nil && !isNone(caFile) {
		if why := checkCAFile(caFile, baseURL); why != "" {
			return entry{}, refuseKey(keyCAFile, why)
		}
	}
	client := clientElement
	if has(d, keyClient) {
		named, ok := text(d, keyClient)
		if !ok || (named != clientElement && named != clientLightning) {
			return entry{}, refuseKey(keyClient, "must be one of "+clientElement+", "+clientLightning)
		}
		client = named
	}
	return entry{team: team, baseURL: baseURL, client: client}, nil
}

// has is whether the entry names key, null included.
func has(d *pycompat.Dict, key string) bool {
	found, ok := d.Get(key)
	return ok && found != nil
}

// value is the entry's key, nil when it names none.
func value(d *pycompat.Dict, key string) pycompat.Value {
	found, ok := d.Get(key)
	if !ok {
		return nil
	}
	return found
}

func text(d *pycompat.Dict, key string) (string, bool) {
	s, ok := value(d, key).(pycompat.Str)
	return string(s), ok
}

func isNone(v pycompat.Value) bool {
	switch v.(type) {
	case pycompat.None:
		return true
	default:
		return false
	}
}

// checkServerName is teamfile.check_server_name: its refusal, "" when there is none.
func checkServerName(v pycompat.Value) string {
	s, ok := v.(pycompat.Str)
	if !ok || len(s) > serverNameMax {
		return fmt.Sprintf("must be a DNS name of at most %d characters", serverNameMax)
	}
	labels := strings.Split(string(s), ".")
	if len(labels) < 2 || slices.ContainsFunc(labels, func(label string) bool { return !dnsLabel.MatchString(label) }) {
		return "must be a lower-case DNS name with at least two labels"
	}
	if labels[len(labels)-1] != serverNameTLD {
		return "must end in ." + serverNameTLD + ", which is never delegated"
	}
	return ""
}

// checkCAFile is element.py _check_ca_file: an absolute, plain path to a file, and only an https
// team has one. It is the refusal, "" when there is none.
func checkCAFile(v pycompat.Value, baseURL string) string {
	if !strings.HasPrefix(strings.ToLower(baseURL), "https:") {
		return "is only for a team whose base_url is https"
	}
	s, ok := v.(pycompat.Str)
	path := string(s)
	if !ok || !strings.HasPrefix(path, "/") || strings.HasSuffix(path, "/") {
		return "must be an absolute path to a file"
	}
	if strings.ContainsAny(path, "\x00\n\r") || slices.Contains(strings.Split(path, "/"), "..") {
		return "must be a plain path, with no parent step or control character"
	}
	return ""
}
