package rules

import (
	"testing"

	"golang.org/x/tools/go/analysis/analysistest"
)

// A seat holding made outside Store.Take, or in a Take that has not asked the session record
// first (the originating form: Take with no Restorable call), is flagged; a Take that asks
// before it holds is not, and a package other than seat is not judged.
func TestSeatHoldUnrecorded(t *testing.T) {
	analysistest.Run(t, analysistest.TestData(), SeatHoldUnrecorded,
		"seathold/bad/internal/teamchat/seat", "seathold/late/internal/teamchat/seat",
		"seathold/good/internal/teamchat/seat")
}
