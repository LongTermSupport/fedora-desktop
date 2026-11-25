package teams

import (
	"errors"
	"testing"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/seat"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teams/teamchatlaunch"
)

// The launch verbs Wire builds open the seat store the configuration names and stamp records
// with the production clock.
func TestTheLaunchVerbsAreWiredToTheStoreAndTheClock(t *testing.T) {
	v := newHarness(t).app.Launch
	if _, ok := v.Taker(seat.Config{}).(teamchatlaunch.StoreTaker); !ok {
		t.Fatal("the taker is not the seat store's")
	}
	if holder, err := v.Holder(seat.Config{}); holder != nil || !errors.Is(err, seat.ErrConfig) {
		t.Fatalf("an incomplete store configuration opened: %v, %v", holder, err)
	}
	if v.NowMS() <= 0 {
		t.Fatal("the clock reads no time")
	}
}
