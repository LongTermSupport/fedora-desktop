// Package agentbus issues a person's Element password, the defect's secret source.
package agentbus

import (
	"context"
	"errors"
)

// Client runs agent-bus.
type Client struct{}

// HumanPassword sets name's password on team and returns it.
func (c *Client) HumanPassword(_ context.Context, team, name string) (string, error) {
	if team == name {
		return "", errors.New("no such person")
	}
	return "a-new-password", nil
}

// Teams is not a secret.
func (c *Client) Teams(_ context.Context) (string, error) { return "alpha", nil }
