package seat

import (
	"context"
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/matrix"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// ErrReissue is a seat whose pool credential is refused at sign-in, or a handle marked
// `needs: reissue`: only `member reissue` mends it (C 3.7.3 take steps 1 and 3).
var ErrReissue = errors.New("seat: the seat needs member reissue")

// signedOut is the token a member client is built with before it signs in; login sends none.
const signedOut = "signed-out"

// Credential is a pool seat's credential (SC1, DC16 (c'), DESIGN-v2.md 7.1): the kind, which
// names its pool file and the key it is issued under; Mint, take step 3, which answers the
// token this take keeps from the pool's secret; Retire, release step 4, given the held token.
// Password is (c'); 7.1's (d1) or (d2) is another implementation, and nothing outside this
// seam names the password.
type Credential interface {
	Kind() Kind
	Mint(ctx context.Context, b Bundle, pool Secret) (Secret, error)
	Retire(ctx context.Context, b Bundle, held Secret) error
}

// Password is the pool password credential (DC16 (c')): a take logs in as the seat's device,
// logs that device out, and logs in again.
type Password struct {
	// Clock is how the member client waits between retries.
	Clock matrix.Clock
}

// Kind is the password.
func (Password) Kind() Kind { return KindPassword }

// Mint logs in as TCSEAT, logs out with the token it answered, which ends every earlier token
// of the device on a server that keeps them (H-C13), and logs in again, answering that token
// (SC2). A first login refused M_FORBIDDEN is ErrReissue.
func (p Password) Mint(ctx context.Context, b Bundle, pool Secret) (Secret, error) {
	c, err := p.client(b, signedOut)
	if err != nil {
		return Secret{}, err
	}
	defer c.Close()
	login := b.Handle().String()
	first, err := c.LoginDevice(ctx, login, pool.Reveal(), paths.SeatDevice)
	if e, failed := errors.AsType[*matrix.Error](err); failed && e.Errcode == "M_FORBIDDEN" {
		return Secret{}, fmt.Errorf("%w: %s refused its pool password, which was reset outside this pool", ErrReissue, b.UserID)
	}
	if err != nil {
		return Secret{}, fmt.Errorf("seat: signing in as %s: %w", b.UserID, err)
	}
	if err = p.logout(ctx, b, first.AccessToken.Reveal()); err != nil {
		return Secret{}, err
	}
	kept, err := c.LoginDevice(ctx, login, pool.Reveal(), paths.SeatDevice)
	if err != nil {
		return Secret{}, fmt.Errorf("seat: signing in again as %s: %w", b.UserID, err)
	}
	return ParseSecret(kept.AccessToken.Reveal())
}

// Retire logs the held token out; a token already dead (M_UNKNOWN_TOKEN) is done (SC3).
func (p Password) Retire(ctx context.Context, b Bundle, held Secret) error {
	err := p.logout(ctx, b, held.Reveal())
	if e, failed := errors.AsType[*matrix.Error](err); failed && e.Errcode == "M_UNKNOWN_TOKEN" {
		return nil
	}
	return err
}

func (p Password) logout(ctx context.Context, b Bundle, token string) error {
	c, err := p.client(b, token)
	if err != nil {
		return err
	}
	defer c.Close()
	if err := c.Logout(ctx); err != nil {
		return fmt.Errorf("seat: signing %s out: %w", b.UserID, err)
	}
	return nil
}

func (p Password) client(b Bundle, token string) (*matrix.Client, error) {
	c, err := matrix.NewMember(matrix.Config{BaseURL: b.BaseURL, PlainHTTPHosts: b.PlainHTTPHosts, Token: token, Clock: p.Clock})
	if err != nil {
		return nil, fmt.Errorf("seat: the member client for %s: %w", b.UserID, err)
	}
	return c, nil
}
