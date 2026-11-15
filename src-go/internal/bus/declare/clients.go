package declare

import (
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/bus/pycompat"
)

// The Matrix clients a team may open in (Plan 00254 DESIGN-client-choice.md), the words of
// agent_bus_matrix_client and of an agent_bus_element_teams entry's client.
const (
	ClientElement   = "element"
	ClientLightning = "lightning"
)

// matrixClientKey is the home-level setting: the client every team opens in, unless its own
// agent_bus_element_teams entry names another.
const matrixClientKey = "agent_bus_matrix_client"

// clientKey is an agent_bus_element_teams entry's own client.
const clientKey = "client"

// clients are the values the setting and the entry key may take, in the order a refusal names.
var clients = []string{ClientElement, ClientLightning}

// Clients is the Matrix client each team opens in. It is Go's one definition of the rule
// play-agent-bus-element.yml applies in its first task: a team's own client, else
// agent_bus_matrix_client, else element. The default stays element until Plan 00254 Task 7.1.
type Clients struct {
	// Default is agent_bus_matrix_client, or element when the host_vars do not set it.
	Default string
	// Teams is the client of each agent_bus_element_teams entry that names one.
	Teams map[string]string
}

// Of is the client team opens in: its entry's own, else the default. A team the list does not
// name opens in the default, as it would once ccy-teams declared its entry.
func (c Clients) Of(team string) string {
	if client, named := c.Teams[team]; named {
		return client
	}
	return c.Default
}

// MatrixClients reads agent_bus_matrix_client, a top-level plain scalar, and each
// agent_bus_element_teams entry's client. Any value but element or lightning is refused, as the
// play refuses it, naming the setting or the team and the value; a tagged (vaulted) setting is
// refused, as every reader here refuses one.
func (h HostVars) MatrixClients() (Clients, error) {
	def, err := homeClient(pycompat.SplitLines(string(h), true))
	if err != nil {
		return Clients{}, err
	}
	hv, err := parse(string(h), elementKey)
	if err != nil {
		return Clients{}, err
	}
	out := Clients{Default: def, Teams: map[string]string{}}
	for _, e := range hv.teams {
		if _, named := find(e.raw, clientKey); !named {
			continue
		}
		v, err := rawValue(e, clientKey)
		if err != nil {
			return Clients{}, err
		}
		text, isText := v.(pycompat.Str)
		if !isText || !slices.Contains(clients, string(text)) {
			return Clients{}, refusedf("team %s's %s is %s; %s", e.team, clientKey, pycompat.PyRepr(v), clientRule())
		}
		out.Teams[e.team] = string(text)
	}
	return out, nil
}

// homeClient is agent_bus_matrix_client, element when it is not set.
func homeClient(lines []string) (string, error) {
	var found []string
	for _, line := range lines {
		if rest, ok := keyRest(bare(line), matrixClientKey); ok {
			found = append(found, rest)
		}
	}
	switch {
	case len(found) > 1:
		return "", refusedf("%s is set more than once", matrixClientKey)
	case len(found) == 0:
		return ClientElement, nil
	}
	value, err := scalar(found[0], matrixClientKey)
	if err != nil {
		return "", err
	}
	if !slices.Contains(clients, value) {
		return "", refusedf("%s is %s; %s", matrixClientKey, pycompat.PyRepr(pycompat.Str(value)), clientRule())
	}
	return value, nil
}

func clientRule() string {
	return "the client must be " + strings.Join(clients, " or ")
}
