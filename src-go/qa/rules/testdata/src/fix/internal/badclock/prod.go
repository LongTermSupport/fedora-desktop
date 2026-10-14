// Package badclock reads the clock in production code, which is not this rule's business.
package badclock

import "time"

// Stamp is production code: time.Now here is fine.
func Stamp() time.Time { return time.Now() }
