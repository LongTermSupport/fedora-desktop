package proto

import (
	"errors"
	"testing"
)

func TestRefusalCarriesItsReason(t *testing.T) {
	var err error = &RefusalError{Reason: ReasonTarget, Detail: "too many"}
	var r *RefusalError
	if !errors.As(err, &r) || r.Reason != ReasonTarget || err.Error() != "target: too many" {
		t.Fatalf("%v", err)
	}
	if refuse(ReasonSchema).Error() != "schema" {
		t.Fatal("a refusal without detail is its reason")
	}
	if (&UntrustedError{Reason: "humans"}).Error() != "the team record is untrusted: humans" {
		t.Fatal("untrusted text")
	}
}

func TestClosedReasonSets(t *testing.T) {
	if len(DropReasons) != 18 || DropReasons[0] != ReasonVersion || DropReasons[17] != ReasonProvenance {
		t.Fatalf("drop reasons %v", DropReasons)
	}
	if len(IgnoreReasons) != 8 || len(SendRefusals) != 2 {
		t.Fatal("ignore or send sets")
	}
}

func TestContextMembership(t *testing.T) {
	ctx := &Context{Humans: []string{"@a:s"}}
	if !ctx.IsHuman("@a:s") || ctx.IsHuman("@b:s") {
		t.Fatal("IsHuman")
	}
}
