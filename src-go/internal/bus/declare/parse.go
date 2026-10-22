package declare

import (
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
)

// The host_vars keys the reader reads (declare.py TEAMS_KEY, LXC_KEY, ELEMENT_KEY,
// ADDRESS_KEY) and the team file's defaults (teamfile.py SERVER_NAME_SUFFIX, STATES).
const (
	teamsKey         = "agent_bus_teams"
	lxcKey           = "agent_bus_lxc_members"
	elementKey       = "agent_bus_element_teams"
	addressKey       = "agent_bus_address"
	serverNameSuffix = ".agent-bus.internal"
	statePresent     = "present"
	stateAbsent      = "absent"
	keyTeam          = "team"
	keyRepos         = "repos"
)

// rawKey is one key of an entry as written: its name, the rest of its line, the line's index
// and the lines nested under it (declare.py's Declared.raw).
type rawKey struct {
	key    string
	rest   string
	index  int
	nested []string
}

// entry is one item of a top-level list, as declare.py's _declared reads it.
type entry struct {
	team       string
	port       int
	hasPort    bool
	state      string
	serverName string
	listen     string // the first listen address of a flow list, or ""
	raw        []rawKey
}

// hostVars is what declare.py's parse returns that the queries use.
type hostVars struct {
	teams   []entry
	address string // the canonical bus address, or ""
}

// parse is declare.py's parse (SPEC-view.md V2-V5): the entries of the top-level list listKey
// and the bus address, every other shape refused naming its line.
func parse(text, listKey string) (hostVars, error) {
	lines := pycompat.SplitLines(text, true)
	var keys []int
	var rests []string
	for i, line := range lines {
		b := bare(line)
		if strings.Contains(b, listKey) && !strings.HasPrefix(pycompat.LStrip(b), "#") {
			rest, ok := keyRest(b, listKey)
			if !ok {
				return hostVars{}, refusedf("line %d names %s in a form ccy-teams does not edit", i+1, listKey)
			}
			keys, rests = append(keys, i), append(rests, rest)
		}
	}
	if len(keys) > 1 {
		numbers := make([]string, 0, len(keys))
		for _, k := range keys {
			numbers = append(numbers, strconv.Itoa(k+1))
		}
		return hostVars{}, refusedf("%s is set more than once (lines %s)", listKey, strings.Join(numbers, ", "))
	}
	address, err := busAddress(lines)
	if err != nil || len(keys) == 0 {
		return hostVars{address: address}, err
	}
	value, err := scalar(rests[0], listKey)
	if err != nil {
		return hostVars{}, err
	}
	if value != "" {
		return hostVars{}, refusedf("%s on line %d is written inline; ccy-teams adds only to a block list (as in localhost.yml.dist)", listKey, keys[0]+1)
	}
	content, err := listLines(lines, keys[0])
	if err != nil || len(content) == 0 {
		return hostVars{address: address}, err
	}
	teams, err := items(lines, content, listKey)
	return hostVars{teams: teams, address: address}, err
}

// busAddress is declare.py's _address (V5): the canonical form of the one agent_bus_address,
// "" when there is none or it is tagged or not an IP address.
func busAddress(lines []string) (string, error) {
	var found []string
	for _, line := range lines {
		if rest, ok := keyRest(bare(line), addressKey); ok {
			found = append(found, rest)
		}
	}
	if len(found) > 1 {
		return "", refusedf("%s is set more than once", addressKey)
	}
	if len(found) == 0 {
		return "", nil
	}
	if value, ok := plainScalar(found[0]); ok {
		if address, ok := pycompat.IPAddress(value); ok {
			return address, nil
		}
	}
	return "", nil
}

// listLines is the indices of the list's content lines after its key line (V4): lines
// indented with a space or starting "- ", blank and comment lines skipped, a tab in the
// leading whitespace refused; the first other line ends the list.
func listLines(lines []string, key int) ([]int, error) {
	var content []int
	for index := key + 1; index < len(lines); index++ {
		b := bare(lines[index])
		if strings.Contains(b[:len(b)-len(pycompat.LStrip(b))], "\t") {
			return nil, refusedf("line %d is indented with a tab", index+1)
		}
		if pycompat.Strip(b) == "" || strings.HasPrefix(pycompat.LStrip(b), "#") {
			continue
		}
		if !strings.HasPrefix(b, " ") && !strings.HasPrefix(b, "- ") {
			break
		}
		content = append(content, index)
	}
	return content, nil
}

// items is declare.py's _items, then _unique and _declared: the structure of every line
// first, then each item's team, then each item's values.
func items(lines []string, content []int, listKey string) ([]entry, error) {
	s := itemScan{listKey: listKey, itemIndent: spaces(bare(lines[content[0]])), keyIndent: -1}
	for _, index := range content {
		if err := s.line(bare(lines[index]), index); err != nil {
			return nil, err
		}
	}
	all := s.all
	if err := unique(all, listKey); err != nil {
		return nil, err
	}
	entries := make([]entry, 0, len(all))
	for _, item := range all {
		e, err := declared(item)
		if err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, nil
}

// itemScan is _items' loop state: the items so far, each its keys in order.
type itemScan struct {
	listKey    string
	itemIndent int
	keyIndent  int
	all        [][]rawKey
}

// line judges one content line: an item's first line, a key line of the item, or a line
// nested under the item's last key.
func (s *itemScan) line(b string, index int) error {
	indent := spaces(b)
	body := b[indent:]
	where := fmt.Sprintf("line %d", index+1)
	switch {
	case indent == s.itemIndent && strings.HasPrefix(body, "- "):
		after := body[2:]
		this := s.itemIndent + 2 + spaces(after)
		if s.keyIndent != -1 && s.keyIndent != this {
			return refusedf("%s: the %s items are not indented alike", where, s.listKey)
		}
		s.keyIndent = this
		s.all = append(s.all, nil)
		body = strings.TrimLeft(after, " ")
	case indent == s.keyIndent && len(s.all) > 0:
	case s.keyIndent >= 0 && indent > s.keyIndent && len(s.all) > 0:
		last := s.all[len(s.all)-1]
		if len(last) == 0 {
			return refusedf("%s: an indented line before any key of the item", where)
		}
		last[len(last)-1].nested = append(last[len(last)-1].nested, b)
		return nil
	default:
		return refusedf("%s: not the %s shape ccy-teams edits (a block list of mappings)", where, s.listKey)
	}
	key, rest, ok := keyLine(body)
	if !ok {
		return refusedf("%s: each %s item must be a block mapping of key: value lines", where, s.listKey)
	}
	item := s.all[len(s.all)-1]
	if _, dup := find(item, key); dup {
		return refusedf("%s: %s is set twice in one %s item", where, key, s.listKey)
	}
	s.all[len(s.all)-1] = append(item, rawKey{key: key, rest: rest, index: index})
	return nil
}

// find is the item's key, or false.
func find(item []rawKey, key string) (rawKey, bool) {
	i := slices.IndexFunc(item, func(k rawKey) bool { return k.key == key })
	if i < 0 {
		return rawKey{}, false
	}
	return item[i], true
}

// unique is declare.py's _unique: every item has a team, and no team is declared twice
// (except among the LXC members, which members.parse_members tells apart).
func unique(all [][]rawKey, listKey string) error {
	seen := map[string]bool{}
	for _, item := range all {
		team, ok := find(item, "team")
		if !ok {
			first := item[0].index
			for _, k := range item {
				first = min(first, k.index)
			}
			return refusedf("the %s item at line %d has no team", listKey, first+1)
		}
		name, err := scalar(team.rest, fmt.Sprintf("the team on line %d", team.index+1))
		if err != nil {
			return err
		}
		if seen[name] && listKey != lxcKey {
			return refusedf("team %s is declared twice in %s", name, listKey)
		}
		seen[name] = true
	}
	return nil
}

// declared is declare.py's _declared for an item unique has seen to have a team: its team,
// port, state and server name, each judged in that order.
func declared(item []rawKey) (entry, error) {
	team := item[slices.IndexFunc(item, func(k rawKey) bool { return k.key == keyTeam })]
	name, err := scalar(team.rest, fmt.Sprintf("the team on line %d", team.index+1))
	if err != nil {
		return entry{}, err
	}
	if !teamNameRE.MatchString(name) {
		return entry{}, refusedf("line %d: the team name does not match %s", team.index+1, teamNamePattern)
	}
	e := entry{team: name, state: statePresent, serverName: name + serverNameSuffix, raw: item}
	if k, ok := find(item, "port"); ok {
		text, portErr := scalar(k.rest, fmt.Sprintf("the port on line %d", k.index+1))
		if portErr != nil {
			return entry{}, portErr
		}
		if e.port, err = portNumber(text, k.index+1); err != nil {
			return entry{}, err
		}
		e.hasPort = true
	}
	if k, ok := find(item, "state"); ok {
		if e.state, err = scalar(k.rest, fmt.Sprintf("the state on line %d", k.index+1)); err != nil {
			return entry{}, err
		}
		if e.state != statePresent && e.state != stateAbsent {
			return entry{}, refusedf("line %d: state must be one of present, absent", k.index+1)
		}
	}
	if k, ok := find(item, "server_name"); ok {
		if e.serverName, err = scalar(k.rest, fmt.Sprintf("the server_name on line %d", k.index+1)); err != nil {
			return entry{}, err
		}
	}
	e.listen, err = firstListen(item)
	return e, err
}

// firstListen is declare.py's _first_listen: the first listen address when the list is
// written as JSON and starts with a string; "" otherwise, a tagged listen included. A JSON
// error other than python's JSONDecodeError (an integer past the digit limit, nesting past
// the recursion limit) is the crash it was in v1.
func firstListen(item []rawKey) (string, error) {
	k, ok := find(item, "listen")
	if !ok {
		return "", nil
	}
	text, ok := plainScalar(k.rest)
	if !ok {
		return "", nil
	}
	value, err := pycompat.Loads(text, pycompat.LoadOptions{})
	if err != nil {
		return "", jsonCrash(err, fmt.Sprintf("the listen on line %d", k.index+1))
	}
	if list, isList := value.(pycompat.List); isList && len(list) > 0 {
		if first, isStr := list[0].(pycompat.Str); isStr {
			return string(first), nil
		}
	}
	return "", nil
}

// jsonCrash is nil for python's JSONDecodeError, which v1 catches, and the crash class for
// every other exception json.loads raises.
func jsonCrash(err error, where string) error {
	if pe, ok := errors.AsType[*pycompat.Error](err); ok && pe.Class == pycompat.ClassJSONDecodeError {
		return nil
	}
	return crashf("%s: %v", where, err)
}
