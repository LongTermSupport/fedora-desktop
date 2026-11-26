package seattext

import "testing"

// S1: Finding names each shape of a seat a person is told to type or pick, and passes clean text.
func TestFindingNamesEachShape(t *testing.T) {
	for _, tc := range []struct {
		tmpl     string
		operands []string
		rows     bool
		want     string
	}{
		{"ccy --teams <seat>@<team>", nil, false, FindingLaunch},
		{"cc --teams 1@<team>,2@<team2>", nil, false, FindingLaunch},
		{"--teams SEAT@TEAM", nil, false, FindingLaunch},
		{"ccy --teams dev1@dev-team,qa2@other-team", nil, false, FindingLaunch},
		{"run ccy --teams " + Value + "@dev", []string{"n"}, false, FindingLaunch},
		{"--coordinator=hand-to=<seat>", nil, false, FindingRepair},
		{"ccy-teams seats model 2@<team> opus", nil, false, FindingRepair},
		{"a seat is one <seat> of a team", nil, false, FindingPlaceholder},
		{"prints SEAT@TEAM on stdout", nil, false, FindingPlaceholder},
		{"ccy --teams " + Value, []string{"canonical"}, false, FindingList},
		{"seat " + Value + " free", []string{"s.Name"}, true, FindingRow},
		{Value + " held", []string{"s.Handle"}, true, FindingHandleRow},
		{"ccy --teams <team>", nil, false, ""},
		{"ccy --teams " + Value, []string{"team"}, false, ""},
		{"seat " + Value + " free", []string{"s.Name"}, false, ""},
		{"the handle <repo>.<seat>+<host>.lxc", nil, false, FindingPlaceholder},
	} {
		if got := Finding(tc.tmpl, tc.operands, tc.rows); got != tc.want {
			t.Errorf("Finding(%q, %q, %v) = %q, want %q", tc.tmpl, tc.operands, tc.rows, got, tc.want)
		}
	}
}
