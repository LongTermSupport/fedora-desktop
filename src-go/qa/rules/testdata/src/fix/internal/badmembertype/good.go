package badmembertype

import (
	"context"

	"fix/internal/agentbus"
)

// memberType is the member type of a session of kind.
func memberType(kind string) string {
	if kind == "cc" {
		return "host"
	}
	return "podman"
}

func fromKind(ctx context.Context, bus *agentbus.Client, s session, team string) (agentbus.Choices, error) {
	return bus.SeatChooseIn(ctx, s.Checkout, team, memberType(s.Kind))
}

func passedOn(ctx context.Context, bus *agentbus.Client, team, launcherType string) (agentbus.Choices, error) {
	return bus.SeatChooseAs(ctx, team, launcherType)
}

// constantElsewhere is a constant in a parameter not named memberType.
func constantElsewhere(ctx context.Context, bus *agentbus.Client, kind string) error {
	return bus.SeatTake(ctx, "/checkout", "1@alpha", memberType(kind), true)
}
