package teams

import (
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/declare"
)

func TestApplyWord(t *testing.T) {
	cases := map[declare.Status]string{
		{State: declare.ApplyCurrent}:                             wordUpToDate,
		{State: declare.ApplyOutdated, Detail: "unstamped"}:       "out of date",
		{State: declare.ApplyOutdated, Detail: "config,software"}: "out of date: both",
		{State: declare.ApplyOutdated, Detail: "config"}:          "out of date: config",
		{State: declare.ApplyUnbuilt}:                             wordNotBuilt,
		{State: declare.ApplyRemoved}:                             wordRemoved,
		{State: declare.ApplyDecide, Detail: "absent"}:            "decide: absent",
		{State: declare.ApplyDecide, Detail: "undeclared"}:        "decide: undeclared",
		{State: declare.ApplyUnreadable, Detail: "bad entry"}:     wordUnreadable,
		{State: "something new"}:                                  wordUnknown,
	}
	for st, want := range cases {
		if got := applyWord(st); got != want {
			t.Errorf("%+v: got %q, want %q", st, got, want)
		}
	}
}
