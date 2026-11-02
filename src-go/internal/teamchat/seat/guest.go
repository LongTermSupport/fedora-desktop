package seat

import (
	"errors"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// KeepGuestToken writes the control user's copy of an LXC guest seat's token (SP4,
// paths.GuestTokenCopy under base, which is empty outside tests) by SW1's replace, its guest/
// directory made 0700. The team's control state directory must already be the caller's, 0700.
func KeepGuestToken(base string, team paths.TeamID, seat paths.SeatName, token Secret) error {
	root, err := openDir(base+paths.CtlState(team), false)
	if err != nil {
		return err
	}
	path := base + paths.GuestTokenCopy(team, seat)
	err = root.mkdirAll(filepath.Dir(path))
	if err == nil {
		err = root.replace(path, []byte(token.Reveal()))
	}
	return errors.Join(err, root.close())
}
