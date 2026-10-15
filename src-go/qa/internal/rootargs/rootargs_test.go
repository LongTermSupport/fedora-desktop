package rootargs

import (
	"errors"
	"testing"
)

func TestStrictReadsTheThreeFlags(t *testing.T) {
	got, err := Strict([]string{"--scripts", "/r/s", "--root", "/r", "--help"})
	if err != nil || got != (Args{Help: true, Root: "/r", Scripts: "/r/s"}) {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err = Strict(nil); err != nil || got != (Args{}) {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err = Strict([]string{"-h"}); err != nil || !got.Help {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestStrictRefusesAMissingEmptyOrFlagLikeValue(t *testing.T) {
	for _, args := range [][]string{{"--root"}, {"--scripts", ""}, {"--root", ""}, {"--root", "--scripts"}} {
		_, err := Strict(args)
		if err == nil || err.Error() != args[0]+" needs a directory" {
			t.Fatalf("%v: %v", args, err)
		}
	}
}

func TestStrictReturnsATypedUnknownArgumentError(t *testing.T) {
	_, err := Strict([]string{"--bogus"})
	unknown, typed := errors.AsType[*UnknownArgError](err)
	if !typed || unknown.Arg != "--bogus" || err.Error() != "unknown argument '--bogus'" {
		t.Fatalf("%v", err)
	}
}
