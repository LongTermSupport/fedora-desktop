package rootargs

import (
	"errors"
	"testing"
)

func TestLenientReadsTheThreeFlags(t *testing.T) {
	got, err := Lenient([]string{"--root", "/r", "--scripts", "/r/s", "-h"})
	if err != nil || got != (Args{Help: true, Root: "/r", Scripts: "/r/s"}) {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err = Lenient(nil); err != nil || got != (Args{}) {
		t.Fatalf("%+v %v", got, err)
	}
	if got, err = Lenient([]string{"--help"}); err != nil || !got.Help {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestLenientTakesAnyNextWordAsTheValueAndNamesWhatIsWrong(t *testing.T) {
	if got, err := Lenient([]string{"--root", "--scripts"}); err != nil || got.Root != "--scripts" {
		t.Fatalf("%+v %v", got, err)
	}
	if _, err := Lenient([]string{"--root"}); err == nil || err.Error() != "--root needs a value" {
		t.Fatalf("%v", err)
	}
	_, err := Lenient([]string{"--bogus"})
	if err == nil || err.Error() != "unknown argument '--bogus'" {
		t.Fatalf("%v", err)
	}
	if unknown, typed := errors.AsType[*UnknownArgError](err); typed && unknown != nil {
		t.Fatal("the lenient grammar reports an unknown argument as a plain error")
	}
}

func TestStrictReadsTheThreeFlags(t *testing.T) {
	got, err := Strict([]string{"--scripts", "/r/s", "--root", "/r", "--help"})
	if err != nil || got != (Args{Help: true, Root: "/r", Scripts: "/r/s"}) {
		t.Fatalf("%+v %v", got, err)
	}
}

func TestStrictRefusesAMissingEmptyOrFlagLikeValue(t *testing.T) {
	for _, args := range [][]string{{"--root"}, {"--scripts", ""}, {"--root", "--scripts"}} {
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
