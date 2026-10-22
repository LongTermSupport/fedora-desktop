package seat

import (
	"errors"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// PlaceGuest places an LXC guest's issued bundle, run as the agent user in the guest (SP3): a
// fresh `<team>.new-<claim>/` (0700) under the layout's state root with `token`, then
// `seat.json` (0600), swapped in as `<team>/`, the old directory removed whole without
// following a link. Any `<team>.new-*` an earlier placement left is removed first, since a
// guest has no quarantine and it may hold a token. Only a token bundle is placed.
func PlaceGuest(layout paths.Layout, is Issued) error {
	if is.Kind != KindToken {
		return invalid("issued bundle: a guest seat takes a token, not a " + string(is.Kind))
	}
	data, err := is.Seat.Encode()
	if err != nil {
		return err
	}
	root, err := openDir(layout.State(), true)
	if err != nil {
		return err
	}
	return errors.Join(placeGuest(root, layout, is, data), root.close())
}

func placeGuest(root *dir, layout paths.Layout, is Issued, data []byte) error {
	if err := removeGuestClaims(root, is.Seat.Team); err != nil {
		return err
	}
	claim, err := newClaim()
	if err != nil {
		return err
	}
	fresh := layout.SeatClaim(is.Seat.Team, claim).Dir()
	if err = root.mkdirFresh(fresh); err != nil {
		return err
	}
	if err = root.create(fresh+"/token", []byte(is.Secret.Reveal())); err != nil {
		return err
	}
	if err = root.create(fresh+"/seat.json", data); err != nil {
		return err
	}
	seat := layout.Seat(is.Seat.Team).Dir()
	err = root.rename(fresh, seat, unix.RENAME_NOREPLACE)
	if !errors.Is(err, unix.EEXIST) {
		return err
	}
	if err = root.rename(fresh, seat, unix.RENAME_EXCHANGE); err != nil {
		return err
	}
	return os.RemoveAll(fresh)
}

// removeGuestClaims removes every `<team>.new-*` under the guest's state root.
func removeGuestClaims(root *dir, team paths.TeamID) error {
	names, err := root.entries(root.path)
	if err != nil {
		return err
	}
	for _, name := range names {
		if strings.HasPrefix(name, team.String()+".new-") {
			if err := os.RemoveAll(root.path + "/" + name); err != nil {
				return err
			}
		}
	}
	return nil
}

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
