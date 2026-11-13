package badstart

import (
	"fix/internal/conversation"
	"fix/internal/registry"
)

// asked takes the planner's decision.
func asked(rec registry.Record) ([]string, error) {
	start, err := conversation.StartPlan(rec)
	if err != nil {
		return nil, err
	}
	return start.Args, nil
}

// looked reads the recorded conversation without deciding a start (a compact, a report).
func looked(rec registry.Record) string {
	id, err := conversation.For(rec)
	if err != nil {
		return ""
	}
	return id
}
