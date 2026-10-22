//go:build tuwunel

package ctl

// Bootstrap step 1 against a real Tuwunel (SPEC M44): `go test -tags tuwunel` with
// FAKEHS_TUWUNEL_BIN naming the binary. It runs on the wall clock, as a real server starts and
// answers in real time.

import (
	"context"
	"net/url"
	"os"
	"slices"
	"strconv"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/tuwunelhs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

func portOf(t *testing.T, base string) int {
	t.Helper()
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("Base: %v", err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("port: %v", err)
	}
	return port
}

func TestTuwunelRegistersTheAdminAndStoresItsToken(t *testing.T) {
	team, err := paths.ParseTeamID("alpha")
	if err != nil {
		t.Fatalf("ParseTeamID: %v", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 2*tuwunelhs.StartLimit)
	defer cancel()
	hs := tuwunelhs.Start(ctx, t, paths.ServerName(team))
	port := portOf(t, hs.Base)
	root := t.TempDir()
	dir := root + paths.CtlState(team)
	if mkErr := os.MkdirAll(dir, dirMode); mkErr != nil {
		t.Fatalf("MkdirAll: %v", mkErr)
	}
	cfg := Config{Root: root}
	o, err := Open(ctx, cfg, team, port, hs.RegKey)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	a, ok := o.Admin.(*matrix.Admin)
	if !ok {
		t.Fatalf("Deps.Admin is %T", o.Admin)
	}
	id, err := a.Whoami(ctx)
	if err != nil {
		t.Fatalf("Whoami: %v", err)
	}
	adminID := "@admin:" + paths.ServerName(team)
	if id.UserID != adminID || id.DeviceID != AdminDevice {
		t.Errorf("whoami = %+v", id)
	}
	list, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	if len(list) != 1 || list[0].Name() != tokenName {
		t.Errorf("ctl/<team>/ = %v, want only admin.token", list)
	}
	if _, againErr := Open(ctx, cfg, team, port, hs.RegKey); againErr != nil {
		t.Fatalf("a second apply: %v", againErr)
	}
	admins, err := a.ServerAdmins(ctx)
	if err != nil {
		t.Fatalf("ServerAdmins: %v", err)
	}
	if !slices.Contains(admins, adminID) {
		t.Errorf("server admins = %v", admins)
	}
}
