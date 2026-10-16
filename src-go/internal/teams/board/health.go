package board

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// healthDir is where the root health check (agent-bus-hs-health@<team>.timer, run by
// helpers/agent_bus/health.py) records each team's problems, beside the applied stamps and
// readable by the same group.
const healthDir = "health"

// healthRecord is the part of the record the board shows: the problems, each with what to do,
// and when the record stops being current (the check runs every minute).
type healthRecord struct {
	Checked  int64 `json:"checked"`
	Expires  int64 `json:"expires"`
	Problems []struct {
		What string `json:"what"`
		Fix  string `json:"fix"`
	} `json:"problems"`
}

// healthWords is the team row's health, for an installed team: every problem the check
// recorded with what to do, or why there is no current record. Empty when there is nothing to
// say.
func healthWords(appliedDir, team string, now time.Time) string {
	path := filepath.Join(appliedDir, healthDir, team+".json")
	data, err := os.ReadFile(filepath.Clean(path))
	switch {
	case errors.Is(err, os.ErrNotExist):
		return "health: never checked: ccy-teams apply " + team + " starts agent-bus-hs-health@" + team + ".timer"
	case err != nil:
		return "health: record unreadable: " + err.Error()
	}
	var rec healthRecord
	if err := json.Unmarshal(data, &rec); err != nil {
		return "health: record unreadable: " + path + ": " + err.Error()
	}
	var words []string
	if now.Unix() > rec.Expires {
		words = append(words, "the check last ran "+agoWords(now.Unix()-rec.Checked)+
			" ago: systemctl status agent-bus-hs-health@"+team+".timer --no-pager")
	}
	for _, p := range rec.Problems {
		words = append(words, p.What+": "+p.Fix)
	}
	if len(words) == 0 {
		return ""
	}
	return "health: " + strings.Join(words, "; ")
}

// agoWords is a duration in seconds in the largest whole unit that reads well.
func agoWords(seconds int64) string {
	switch {
	case seconds < 120:
		return strconv.FormatInt(seconds, 10) + " s"
	case seconds < 3600:
		return strconv.FormatInt(seconds/60, 10) + " min"
	case seconds < 86400:
		return strconv.FormatInt(seconds/3600, 10) + " h"
	}
	return strconv.FormatInt(seconds/86400, 10) + " d"
}
