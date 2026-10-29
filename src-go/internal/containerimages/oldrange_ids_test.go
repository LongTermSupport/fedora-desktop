package containerimages

import (
	"strings"
	"testing"
)

const subuidText = "someone:100000:65536\njoe:524288:65536\n"

func TestSubIDRangeIsTheUsersOneLineByNameOrUID(t *testing.T) {
	for _, text := range []string{subuidText, "someone:100000:65536\n1000:524288:65536\n"} {
		got, err := parseSubIDs(text, "joe", 1000)
		if err != nil {
			t.Fatal(err)
		}
		if got != (Range{Start: 524288, Count: 65536}) {
			t.Fatalf("parseSubIDs = %+v", got)
		}
	}
}

func TestSubIDRangeRefusesNoneOrTwo(t *testing.T) {
	for _, text := range []string{"someone:100000:65536\n", "joe:1:2\njoe:3:4\n", "joe:x:65536\n", "joe:1:0\n"} {
		if got, err := parseSubIDs(text, "joe", 1000); err == nil {
			t.Errorf("parseSubIDs(%q) = %+v, want a refusal", text, got)
		}
	}
}

func TestShiftKeepsTheOffsetInTheCurrentRange(t *testing.T) {
	old, cur := Range{Start: 100000, Count: 65536}, Range{Start: 524288, Count: 65536}
	if got, ok := shift(100099, old, cur); !ok || got != 524387 {
		t.Fatalf("shift(100099) = %d, %v", got, ok)
	}
	if _, ok := shift(99999, old, cur); ok {
		t.Fatal("an id below the old range was shifted")
	}
	if _, ok := shift(100010, old, Range{Start: 524288, Count: 10}); ok {
		t.Fatal("an offset past the current range's size was shifted")
	}
}

func owners() Owners {
	return Owners{UID: 1000, GID: 1000, SubUID: Range{Start: 524288, Count: 65536}, SubGID: Range{Start: 524288, Count: 65536}}
}

func TestForeignIsAnyUIDButRootTheUserAndTheCurrentRange(t *testing.T) {
	o := owners()
	for uid, want := range map[int64]bool{0: false, 1000: false, 524288: false, 589823: false, 589824: true, 100099: true, 65534: true} {
		if got := o.foreignUID(uid); got != want {
			t.Errorf("foreignUID(%d) = %v, want %v", uid, got, want)
		}
	}
}

func TestMoveIsTheNewOwnerOfAnOldRangePath(t *testing.T) {
	o, old := owners(), Range{Start: 100000, Count: 65536}
	uid, gid, err := o.move(100099, 100104, old)
	if err != nil || uid != 524387 || gid != 524392 {
		t.Fatalf("move = %d, %d, %v", uid, gid, err)
	}
	// A gid outside the old range is kept: only the uid stops the rootless user deleting.
	if _, gid, err := o.move(100099, 1000, old); err != nil || gid != 1000 {
		t.Fatalf("move with the user's gid = %d, %v", gid, err)
	}
	for _, c := range [][2]int64{{65534, 65534}, {100099, 100000 + 70000}} {
		if _, _, err := o.move(c[0], c[1], Range{Start: 100000, Count: 80000}); err == nil {
			t.Errorf("move(%d, %d) shifted an owner that does not fit", c[0], c[1])
		}
	}
}

func TestOldRangeMustNotOverlapWhatIsMapped(t *testing.T) {
	o := owners()
	if err := o.checkOld(Range{Start: 100000, Count: 65536}); err != nil {
		t.Fatal(err)
	}
	for _, old := range []Range{
		{Start: 500000, Count: 30000}, {Start: 0, Count: 10}, {Start: 999, Count: 2}, {Start: 1, Count: 0},
	} {
		if err := o.checkOld(old); err == nil {
			t.Errorf("checkOld(%+v) passed", old)
		}
	}
	gidClash := o
	gidClash.SubGID = Range{Start: 100000, Count: 10}
	if err := gidClash.checkOld(Range{Start: 100000, Count: 65536}); err == nil || !strings.Contains(err.Error(), "subgid") {
		t.Errorf("an old range over the subgid range = %v", err)
	}
}
