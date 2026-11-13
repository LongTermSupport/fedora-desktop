package login

import (
	"context"
	"crypto/subtle"
	"fmt"
	"strings"

	"fix/internal/term"
)

// shownOnce is the fix: the password goes only to the secret screen.
func (l *Login) shownOnce(ctx context.Context) error {
	password, err := l.Bus.HumanPassword(ctx, "alpha", "owner")
	if err != nil {
		l.Kit.ReportErr(err)
		return err
	}
	l.Kit.ShowSecret(fmt.Sprintf("  %-11s %s\n", "Password", password))
	l.Kit.Tell("Shown on a screen that is cleared when you press Enter.")
	return nil
}

// fed goes to a child on stdin, compared in constant time, measured: none is a terminal write.
func (l *Login) fed(ctx context.Context) error {
	password, err := l.Bus.HumanPassword(ctx, "alpha", "owner")
	in := strings.NewReader(password)
	same := subtle.ConstantTimeCompare([]byte(password), []byte("x")) == 1
	l.Kit.Tell(fmt.Sprintf("read %d of %d bytes; same %v", in.Len(), len(password), same))
	l.Kit.Tell(term.AbbrevHome(strings.Repeat("*", 3), "/home"))
	return err
}

// teams is not a secret source: printing it is fine.
func (l *Login) teams(ctx context.Context) error {
	names, err := l.Bus.Teams(ctx)
	l.Kit.Tell("teams: " + names)
	return err
}
