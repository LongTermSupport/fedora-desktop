package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A person never types or picks a seat number (Plan 00265 R90). The fixtures are the ways a seat
// reaches text a person acts on (a launch spelt out by concatenation, by a format and by a
// placeholder constant, a hand-over, a seat repair, a filled-in seat list, a row labelled by its
// seat, a row showing a handle) and the clean forms beside them (the launch by team, an argv, a
// seat named for diagnosis, a row in words); a package outside the trees completes the picture.
func TestSeatNumberText(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SeatNumberText,
		"fix/internal/teams/badseattext", "fix/internal/teams/cleanseattext", "fix/internal/outsideseattext")
}
