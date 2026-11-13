package login

import (
	"context"
	"fmt"
)

// A test may print a fake secret: test files are not judged.
func printsInATest(l *Login) error {
	password, err := l.Bus.HumanPassword(context.Background(), "alpha", "owner")
	fmt.Println(password)
	return err
}
