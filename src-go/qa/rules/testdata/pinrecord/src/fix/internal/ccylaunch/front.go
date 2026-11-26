// Package ccylaunch asks through a ui prompt before it records.
package ccylaunch

import (
	"context"

	"fix/internal/checkoutpin"
)

// lines is a prompt.
type lines struct{}

// YesNo asks.
func (lines) YesNo(_ context.Context, _ string, _ bool) (bool, error) {
	return false, nil
}

// Take asks, then records.
func Take(ctx context.Context, s checkoutpin.Store, digest string) error {
	var l lines
	yes, err := l.YesNo(ctx, "Take them?", false)
	if err != nil || !yes {
		return err
	}
	return s.Record(digest)
}

// Launder records unasked.
func Launder(s checkoutpin.Store, digest string) error {
	return s.Record(digest) // want `an approval recorded where no person was asked`
}
