package proto

import (
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

func refusalReason(err error) Reason {
	if r, ok := errors.AsType[*RefusalError](err); ok {
		return r.Reason
	}
	return ""
}

func TestCheckReportText(t *testing.T) {
	if headline, err := CheckReportText("Built it\nall green"); err != nil || headline != "Built it" {
		t.Fatal(headline, err)
	}
	for _, c := range []struct {
		text string
		want Reason
	}{
		{" \t\nbody", ReasonBody},
		{strings.Repeat("h", 121), ReasonBody},
		{strings.Repeat("x", 1501), ReasonBody},
		{"ok\n\xed\xa0\x80", ReasonBody},
		{"ok\nghp_" + strings.Repeat("a", 36), RefuseSecret},
	} {
		if _, err := CheckReportText(c.text); refusalReason(err) != c.want {
			t.Errorf("%q: %v, want %s", c.text, err, c.want)
		}
	}
}

func TestCheckThreadText(t *testing.T) {
	if err := CheckThreadText("", true); err != nil {
		t.Fatal(err)
	}
	if err := CheckThreadText("", false); refusalReason(err) != ReasonBody {
		t.Fatal(err)
	}
}

func TestReportContentRoundTrip(t *testing.T) {
	report := BuildReport("Built it")
	if err := CheckReportContent(report); err != nil {
		t.Fatal(err)
	}
	report["agent_bus.report"] = pyjson.Object{"v": true}
	if refusalReason(CheckReportContent(report)) != ReasonSchema {
		t.Fatal("a bool version was accepted")
	}
}
