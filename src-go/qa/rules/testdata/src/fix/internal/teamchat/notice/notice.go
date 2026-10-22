// Package notice stands in for teamchat's notice package: the client a lifecycle notice is
// read and posted through, and the two calls that read main and post.
package notice

import "context"

// Client is the member client's part a notice uses.
type Client interface {
	Messages(ctx context.Context, room string) ([]string, error)
	Send(ctx context.Context, room, txn, body string) (string, error)
	RoomState(ctx context.Context, room string) ([]string, error)
}

// LatestOwn is the member's latest own lifecycle notice in main.
func LatestOwn(ctx context.Context, c Client, room string) (string, error) {
	page, err := c.Messages(ctx, room)
	if err != nil || len(page) == 0 {
		return "", err
	}
	return page[0], nil
}

// Disconnected posts disconnected unless main already says goodbye.
func Disconnected(ctx context.Context, c Client, room string) error {
	latest, err := LatestOwn(ctx, c, room)
	if err != nil || latest == "disconnected" {
		return err
	}
	_, err = c.Send(ctx, room, "tc-disc", "disconnected")
	return err
}
