package placeissued

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/exits"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

func TestReportOfNilIsOKAndWritesNothing(t *testing.T) {
	var stderr bytes.Buffer
	if code := Report(&stderr, nil); code != term.ExitOK || stderr.Len() != 0 {
		t.Errorf("exit %d, stderr %q", code, stderr.String())
	}
}

func TestReportPrintsTheCauseLineThenTheObjectLast(t *testing.T) {
	f := &FailureError{Object: exits.New(exits.CodeBundle, nil, msgInvalid), Cause: "one\ntwo\x1b[0m three"}
	code, stderr, obj := reported(t, f)
	if code != exits.ExitConfig {
		t.Errorf("exit %d", code)
	}
	want := "teamchat: one two [0m three\n" + string(append(obj.Object().Compact(), '\n'))
	if stderr != want {
		t.Errorf("stderr %q\nwant   %q", stderr, want)
	}
	if obj.Code != exits.CodeBundle || obj.Message != string(msgInvalid) {
		t.Errorf("object %+v", obj)
	}
}

func TestReportQuotesNoTextOfTheDocument(t *testing.T) {
	f := &FailureError{Object: exits.New(exits.CodeBundle, nil, msgInvalid), Cause: `json: unknown field "leakmarker" and "another" one`}
	_, stderr, obj := reported(t, f)
	if want := "teamchat: json: unknown field \"?\" and \"?\" one\n"; !strings.HasPrefix(stderr, want) {
		t.Errorf("stderr %q does not start %q", stderr, want)
	}
	if obj.Code != exits.CodeBundle {
		t.Errorf("object %+v", obj)
	}
}

func TestReportOfAFailureWithNoCauseIsTheObjectAlone(t *testing.T) {
	f := &FailureError{Object: exits.New(exits.CodeBundle, nil, msgEmpty)}
	_, stderr, obj := reported(t, f)
	if want := string(append(obj.Object().Compact(), '\n')); stderr != want {
		t.Errorf("stderr %q, want only the object %q", stderr, want)
	}
}

func TestReportOfAnUnmappedErrorIsTheInternalExit(t *testing.T) {
	code, stderr, obj := reported(t, errors.New("the disk is full"))
	if code != exits.ExitInternal || obj.Code != exits.CodeInternal {
		t.Errorf("exit %d object %+v", code, obj)
	}
	if want := "teamchat: the disk is full\n"; !strings.HasPrefix(stderr, want) {
		t.Errorf("stderr %q does not start with the diagnostic line", stderr)
	}
}

func TestAFailureIsItsObjectAndKeepsWhatCausedIt(t *testing.T) {
	cause := errors.New("the cause")
	f := &FailureError{Object: exits.New(exits.CodeStore, nil, msgStore), err: cause}
	if f.Error() != string(msgStore) {
		t.Errorf("Error() = %q", f.Error())
	}
	if !errors.Is(f, cause) {
		t.Error("the failure does not unwrap to what caused it")
	}
	if got, ok := errors.AsType[*exits.Error](f); !ok || got != f.Object {
		t.Error("the failure does not unwrap to its error object")
	}
}
