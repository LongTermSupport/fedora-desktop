package fsx

import (
	"os"
	"os/user"
	"strconv"
	"testing"
)

func TestLookupOwnerRoot(t *testing.T) {
	o, err := LookupOwner("root", "root")
	if err != nil {
		t.Fatal(err)
	}
	if o != (Owner{UID: 0, GID: 0}) {
		t.Fatalf("root:root = %+v", o)
	}
}

func TestLookupOwnerRefusesUnknown(t *testing.T) {
	cases := [][2]string{
		{"no-such-user-xyz", "root"},
		{"root", "no-such-group-xyz"},
		{"", "root"},
		{"root", ""},
		{"-1", "root"},
	}
	for _, c := range cases {
		if _, err := LookupOwner(c[0], c[1]); err == nil {
			t.Errorf("LookupOwner(%q, %q) accepted", c[0], c[1])
		}
	}
}

func TestCurrentOwnerIsTheRunningUser(t *testing.T) {
	o, err := CurrentOwner()
	if err != nil {
		t.Fatal(err)
	}
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	if strconv.Itoa(o.UID) != u.Uid || strconv.Itoa(o.GID) != u.Gid || o.UID != os.Getuid() {
		t.Fatalf("CurrentOwner = %+v, user %+v", o, u)
	}
}
