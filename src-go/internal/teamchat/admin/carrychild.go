package admin

import (
	"context"
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/carry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// CarryChild is carry.Child (SC-C1 to SC-C3): each op asked of the teamchat-ctl child through
// Live, its answer decoded strictly and fitted to the op (RC6's rule).
type CarryChild struct {
	Live Child
}

var _ carry.Child = CarryChild{}

// Tail implements carry.Child: carry.tail (SC-C1).
func (c CarryChild) Tail(ctx context.Context, team paths.TeamID, teamFile []byte) (carry.Tail, error) {
	ans, err := askOp(ctx, c.Live, OpCarryTail, team, teamFile)
	if err != nil {
		return carry.Tail{}, err
	}
	return carry.Tail{Rooms: ans.Tail, Epoch: *ans.Epoch}, nil
}

// Check implements carry.Child: carry.check on the instance's loopback port (SC-C3); the
// answer holds one entry per tail event asked.
func (c CarryChild) Check(ctx context.Context, team paths.TeamID, a carry.CheckArgs) (carry.Presence, error) {
	if a.Port == (paths.Port{}) {
		return carry.Presence{}, errors.New("carry.check needs the port of the instance under check")
	}
	ans, err := askOp(ctx, c.Live, OpCarryCheck, team, a.TeamFile,
		proto.M(keyPort, proto.Int(int64(a.Port.Int()))), proto.M(keyTail, carry.EncodeTail(a.Tail)))
	if err != nil {
		return carry.Presence{}, err
	}
	if len(ans.Found) != len(a.Tail) {
		return carry.Presence{}, fmt.Errorf("the %s child answered %d tail entries of %d", paths.CtlUser, len(ans.Found), len(a.Tail))
	}
	return carry.Presence{Found: ans.Found, Epoch: *ans.Epoch}, nil
}

// Place implements carry.Child: carry.place (SC-C2), the roster and rooms in their files'
// own JSON, each guest token and lost export; the answer is the names written.
func (c CarryChild) Place(ctx context.Context, team paths.TeamID, a carry.PlaceArgs) ([]string, error) {
	roster, rerr := objectOf(a.Roster)
	rooms, oerr := objectOf(a.Rooms)
	if err := errors.Join(rerr, oerr); err != nil {
		return nil, fmt.Errorf("the carried roster or rooms file: %w", err)
	}
	guests := make([]proto.Value, 0, len(a.Guest))
	for _, g := range a.Guest {
		guests = append(guests, proto.Obj(proto.M("name", proto.Str(g.Handle)), proto.M("data", proto.Str(g.Token))))
	}
	lost := make([]proto.Value, 0, len(a.Lost))
	for _, l := range a.Lost {
		lost = append(lost, proto.Obj(proto.M("n", proto.Int(l.N)), proto.M("data", proto.Str(string(l.Data)))))
	}
	ans, err := askOp(ctx, c.Live, OpCarryPlace, team, a.TeamFile,
		proto.M(keyRoster, roster), proto.M(keyRooms, rooms), proto.M(keyToken, proto.Str(a.Token)),
		proto.M(keyGuest, proto.Arr(guests...)), proto.M(keyLost, proto.Arr(lost...)))
	return ans.Changed, err
}

// objectOf is raw parsed, which must be a JSON object.
func objectOf(raw []byte) (proto.Value, error) {
	v, err := proto.Parse(raw)
	if err == nil && v.Kind() != proto.KindObject {
		err = errors.New("not an object")
	}
	return v, err
}
