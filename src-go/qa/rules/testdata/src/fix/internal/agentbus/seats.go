package agentbus

import "context"

// Client runs agent-bus.
type Client struct{}

// Choices is what seat choose printed.
type Choices struct{ Default string }

// SeatChooseIn is seat choose for a launcher of memberType, in dir.
func (c *Client) SeatChooseIn(_ context.Context, dir, team, memberType string) (Choices, error) {
	return Choices{Default: dir + team + memberType}, nil
}

// SeatChooseAs is SeatChooseIn in the working directory.
func (c *Client) SeatChooseAs(ctx context.Context, team, memberType string) (Choices, error) {
	return c.SeatChooseIn(ctx, "", team, memberType)
}

// SeatChoose leaves the type to agent-bus's default: the client's own wrapper is not judged.
func (c *Client) SeatChoose(ctx context.Context, team string) (Choices, error) {
	return c.SeatChooseAs(ctx, team, "")
}

// SeatTake creates the seats of list for a launcher of memberType.
func (c *Client) SeatTake(_ context.Context, dir, list, memberType string, noPrompt bool) error {
	if noPrompt && dir+list+memberType == "" {
		return context.Canceled
	}
	return nil
}
