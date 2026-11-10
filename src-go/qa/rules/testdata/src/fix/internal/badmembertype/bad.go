// Package badmembertype asks agent-bus for a session's seats with a member type fixed in the
// code, which a session of the other kind is not.
package badmembertype

import (
	"context"

	"fix/internal/agentbus"
)

const typeHost = "host"

type session struct{ Kind, Checkout string }

// askSeat is the picker's as it was: a cc session was offered ccy's podman seats.
func askSeat(ctx context.Context, bus *agentbus.Client, s session, team string) (agentbus.Choices, error) {
	return bus.SeatChooseIn(ctx, s.Checkout, team, "podman") // want `the member type of a seat is the session's kind`
}

func named(ctx context.Context, bus *agentbus.Client, team string) (agentbus.Choices, error) {
	return bus.SeatChooseAs(ctx, team, typeHost) // want `the member type of a seat is the session's kind`
}

func defaulted(ctx context.Context, bus *agentbus.Client, team string) (agentbus.Choices, error) {
	return bus.SeatChooseAs(ctx, team, "") // want `the member type of a seat is the session's kind`
}

func take(ctx context.Context, bus *agentbus.Client, dir string) error {
	return bus.SeatTake(ctx, dir, "1@alpha", "pod"+"man", true) // want `the member type of a seat is the session's kind`
}
