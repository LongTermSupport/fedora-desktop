package matrix_test

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix/fakehs"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// TestTheSyncFilterIsProtosByteForByte is gap 5's pin, from outside both packages so neither
// imports the other: the query's filter is proto.SyncFilter(...).Compact(), and the re-add
// form's is proto.SyncFilterReAdd(...).Compact().
func TestTheSyncFilterIsProtosByteForByte(t *testing.T) {
	const server = "example.org"
	srv := fakehs.New(fakehs.Options{ServerName: server, Quirks: fakehs.TuwunelQuirks()})
	if err := srv.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := srv.Close(); err != nil {
			t.Error(err)
		}
	})
	self := srv.AddUser("repo.1+desk.podman", "", false)
	token, err := srv.MintToken(self)
	if err != nil {
		t.Fatal(err)
	}
	c, err := matrix.NewMember(matrix.Config{BaseURL: srv.URL(), PlainHTTPHosts: []string{"127.0.0.1"}, Token: token})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(c.Close)
	main := "!" + strings.Repeat("M", 43)
	r := proto.Roster{
		Humans:  []string{"@alice:" + server},
		Members: map[string]proto.RosterMember{self: {Role: proto.RoleCoordinator, HumanText: true}},
		Rooms:   proto.SideRooms{Questions: "!" + strings.Repeat("Q", 43), Reports: "!" + strings.Repeat("R", 43)},
	}
	a := proto.Anchors{Server: server, Team: "team", MainRoom: main, Self: self}
	for name, filter := range map[string]proto.Value{"sync": proto.SyncFilter(r, a), "re-add": proto.SyncFilterReAdd(r, a)} {
		want := filter.Compact()
		if _, err := c.Sync(t.Context(), matrix.SyncRequest{FilterJSON: json.RawMessage(want)}); err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		reqs := srv.Requests()
		if got := reqs[len(reqs)-1].Query.Get("filter"); got != string(want) {
			t.Errorf("%s: filter sent\n%s\nwant\n%s", name, got, want)
		}
	}
	if err := srv.CheckModelled(); err != nil {
		t.Error(err)
	}
}
