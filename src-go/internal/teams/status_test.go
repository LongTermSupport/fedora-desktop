package teams

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
)

func TestApplyWord(t *testing.T) {
	cases := map[declare.Status]string{
		{State: stCurrent}:                             wordUpToDate,
		{State: stOutdated, Detail: "unstamped"}:       "out of date",
		{State: stOutdated, Detail: "config,software"}: "out of date: both",
		{State: stOutdated, Detail: "config"}:          "out of date: config",
		{State: stUnbuilt}:                             wordNotBuilt,
		{State: stRemoved}:                             wordRemoved,
		{State: stDecide, Detail: "absent"}:            "decide: absent",
		{State: stDecide, Detail: "undeclared"}:        "decide: undeclared",
		{State: stUnreadable, Detail: "bad entry"}:     wordUnreadable,
		{State: "something new"}:                       wordUnknown,
	}
	for st, want := range cases {
		if got := applyWord(st); got != want {
			t.Errorf("%+v: got %q, want %q", st, got, want)
		}
	}
}
