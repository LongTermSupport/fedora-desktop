package teams

import "testing"

func TestTheTeamOpsAreBuiltOverTheAppsAdapters(t *testing.T) {
	h := newHarness(t)
	o := h.app.newTeamOps()
	if o.Declare != h.app.Declare || o.Sudo != h.app.Sudo || o.Bus != h.app.Bus || o.Gum != h.app.Gum ||
		o.Picker != h.app.Picker || o.Refresh == nil || o.NeedCheckout == nil || o.IsTerminal == nil {
		t.Fatalf("teamOps %+v", o)
	}
	if !o.FileExists(h.root) || o.FileExists(h.root+"/nowhere") {
		t.Fatal("FileExists does not follow the file system")
	}
}

func TestTheTeamOpsRefreshFailsWhenTheTeamsCannotBeRead(t *testing.T) {
	h := newHarness(t)
	if _, err := h.app.newTeamOps().Refresh(t.Context()); err == nil {
		t.Fatal("no error from a runner that answers nothing")
	}
}
