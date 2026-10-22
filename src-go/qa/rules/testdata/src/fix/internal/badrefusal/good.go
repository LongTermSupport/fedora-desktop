package badrefusal

import (
	"errors"
	"fmt"

	"fix/internal/declare"
)

// retriesOnlyUsage asks again for a malformed answer and stops on every other kind.
func retriesOnlyUsage(err error) (bool, error) {
	if err == nil {
		return true, nil
	}
	de, ok := errors.AsType[*declare.Error](err)
	if !ok || !errors.Is(err, declare.ErrUsage) {
		return false, err
	}
	fmt.Printf("  %s. Try again.\n", de.Error())
	return false, nil
}

// exitsWithItsCode hands the kind on as the exit code.
func exitsWithItsCode(err error) int {
	if de, ok := errors.AsType[*declare.Error](err); ok {
		return de.Code
	}
	return 1
}

// asksItsKind asks the error itself.
func asksItsKind(err error) bool {
	var de *declare.Error
	return errors.As(err, &de) && de.Is(declare.ErrRefused)
}

// kindOnly never recovers the type: errors.Is is the kind already.
func kindOnly(err error) bool {
	return errors.Is(err, declare.ErrUsage)
}
