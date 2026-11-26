// Package checkoutpin is the store and the verbs that record into it.
package checkoutpin

import "context"

// Store is the approvals.
type Store struct{ Dir string }

// Record approves digest.
func (Store) Record(_ string) error { return nil }

// Command is the verbs.
type Command struct {
	Confirm func(ctx context.Context, question string) (bool, error)
}

// Compose is the defect: an attended launch recorded the files unshown and unasked.
func (c Command) Compose(s Store, digest string) error {
	return s.Record(digest) // want `an approval recorded where no person was asked`
}

// Approve asks first.
func (c Command) Approve(ctx context.Context, s Store, digest string) error {
	yes, err := c.Confirm(ctx, "Approve?")
	if err != nil || !yes {
		return err
	}
	return s.Record(digest)
}

// Later records from a closure that asks nobody.
func Later(s Store) func() error {
	return func() error {
		return s.Record("x") // want `an approval recorded where no person was asked`
	}
}

// WriteLastLaunch records the bytes it writes itself.
func WriteLastLaunch(s Store, text string) error {
	return s.Record(text)
}
