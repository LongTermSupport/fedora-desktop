// Package outsideseattext is outside the trees project_seat_number_text judges: agent-facing
// text may name a seat.
package outsideseattext

func agentHint(seat, team string) string {
	return "teamchat names you as ccy --teams " + seat + "@" + team
}
