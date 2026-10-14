package declare

import "testing"

func TestStatusWord(t *testing.T) {
	cases := map[Status]string{
		{State: ApplyCurrent}:                             WordUpToDate,
		{State: ApplyOutdated, Detail: "unstamped"}:       "out of date",
		{State: ApplyOutdated, Detail: "config,software"}: "out of date: both",
		{State: ApplyOutdated, Detail: "config"}:          "out of date: config",
		{State: ApplyUnbuilt}:                             WordNotBuilt,
		{State: ApplyRemoved}:                             WordRemoved,
		{State: ApplyDecide, Detail: "absent"}:            "decide: absent",
		{State: ApplyDecide, Detail: "undeclared"}:        "decide: undeclared",
		{State: ApplyUnreadable, Detail: "bad entry"}:     WordUnreadable,
		{State: ApplyAbsent}:                              WordUnknown,
		{State: "something new"}:                          WordUnknown,
	}
	for st, want := range cases {
		if got := st.Word(); got != want {
			t.Errorf("%+v: got %q, want %q", st, got, want)
		}
	}
}
