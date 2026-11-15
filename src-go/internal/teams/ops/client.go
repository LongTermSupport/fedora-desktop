package ops

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/lightning"
)

// Client is a Matrix client a team opens in (Plan 00254 DESIGN-client-choice.md), as ccy-teams
// names and opens it. Which one a team opens in is declare's MatrixClients, the one rule in Go.
type Client struct {
	Key  string // the host_vars word: element or lightning
	Name string // as a person reads it
	// Kept is what play-agent-bus-element.yml keeps on this desktop for a team, beside its
	// launcher: Element's profile, or Lightning's own instance (its data, settings and cache).
	Kept string
}

// The clients a team can open in.
var (
	Element   = Client{Key: declare.ClientElement, Name: "Element", Kept: "profile"}
	Lightning = Client{Key: declare.ClientLightning, Name: "Lightning", Kept: "instance"}
)

// ClientNamed is the client of a host_vars word.
func ClientNamed(key string) (Client, error) {
	switch key {
	case declare.ClientElement:
		return Element, nil
	case declare.ClientLightning:
		return Lightning, nil
	}
	return Client{}, fmt.Errorf("ccy-teams cannot open a team in the Matrix client %q", key)
}

// Words is "the Element profile": what the play keeps for a team in c.
func (c Client) Words() string { return "the " + c.Name + " " + c.Kept }

// Launcher is the desktop file the Element play writes for team in c: Element's through
// helpers/agent_bus/element.py, Lightning's through agent-bus-lightning.
func (o *Ops) Launcher(team string, c Client) string {
	if c == Lightning {
		return lightning.Launcher(o.Cfg.Home, team)
	}
	return o.ElementLauncher(team)
}

// ClientOf is the client team opens in, read from the host_vars now.
func (o *Ops) ClientOf(ctx context.Context, team string) (Client, error) {
	clients, err := o.Declare.MatrixClients(ctx)
	if err != nil {
		return Client{}, err
	}
	return ClientNamed(clients.Of(team))
}

// clientGroup is the teams that open in one client.
type clientGroup struct {
	client Client
	teams  []string
}

// byClient is teams by the client each opens in, Element's first, each in the given order.
func byClient(clients declare.Clients, teams []string) ([]clientGroup, error) {
	for _, team := range teams {
		if _, err := ClientNamed(clients.Of(team)); err != nil {
			return nil, err
		}
	}
	var groups []clientGroup
	for _, c := range []Client{Element, Lightning} {
		var in []string
		for _, team := range teams {
			if clients.Of(team) == c.Key {
				in = append(in, team)
			}
		}
		if len(in) > 0 {
			groups = append(groups, clientGroup{client: c, teams: in})
		}
	}
	return groups, nil
}

// setUpWords is what of the groups the Element play writes: "the Element profile for a b and
// the Lightning instance for c".
func setUpWords(groups []clientGroup) string {
	parts := make([]string, 0, len(groups))
	for _, g := range groups {
		parts = append(parts, g.client.Words()+" for "+strings.Join(g.teams, " "))
	}
	return strings.Join(parts, " and ")
}

// InstallWords is the Flatpaks the Element play installs when it sets up teams opening in
// clients: Element Desktop on every run, and Lightning too whenever one of them is Lightning.
func InstallWords(clients ...Client) string {
	if slices.Contains(clients, Lightning) {
		return "Element Desktop and Lightning"
	}
	return "Element Desktop"
}

// installWords is InstallWords for the groups' clients.
func installWords(groups []clientGroup) string {
	clients := make([]Client, 0, len(groups))
	for _, g := range groups {
		clients = append(clients, g.client)
	}
	return InstallWords(clients...)
}

// notSetUp is what is left undone when the Element play did not run for the groups.
func notSetUp(groups []clientGroup, teams []string) string {
	if len(groups) == 1 {
		return strings.Join(teams, " ") + " has no " + groups[0].client.Name + " " + groups[0].client.Kept + " yet"
	}
	return strings.Join(teams, " ") + " is not set up in its Matrix client yet"
}

// unsetWords is what the Element play would have written for the groups: one client's words,
// or the client set-up when they are several.
func unsetWords(groups []clientGroup) string {
	if len(groups) == 1 {
		return groups[0].client.Words()
	}
	return "the Matrix client set-up"
}
