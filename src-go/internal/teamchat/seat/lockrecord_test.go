package seat

import (
	"errors"
	"testing"
)

func TestDecodeLockRecord(t *testing.T) {
	rec, err := DecodeLockRecord([]byte(`{"v":1,"run":"0123456789abcdef","started_ms":5}`))
	if err != nil || rec.Run.String() != "0123456789abcdef" || rec.StartedMS != 5 || rec.LaunchFailed {
		t.Fatalf("DecodeLockRecord = %+v, %v", rec, err)
	}
	rec, err = DecodeLockRecord([]byte(`{"v":1,"run":"0123456789abcdef","started_ms":5,"launch_failed_ms":9}`))
	if err != nil || !rec.LaunchFailed {
		t.Fatalf("a launch failure = %+v, %v", rec, err)
	}
	for _, bad := range []string{
		``, `{}`, `{"v":2,"run":"0123456789abcdef","started_ms":5}`, `{"v":1,"run":"0123456789ABCDEF","started_ms":5}`,
		`{"v":1,"run":"0123456789abcdef","started_ms":5,"x":1}`, `{"v":1,"run":"0123456789abcdef"}`,
	} {
		if _, err := DecodeLockRecord([]byte(bad)); !errors.Is(err, ErrInvalid) {
			t.Errorf("DecodeLockRecord(%q) = %v, want ErrInvalid", bad, err)
		}
	}
}
