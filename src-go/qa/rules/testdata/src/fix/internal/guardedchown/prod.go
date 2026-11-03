// Package guardedchown has the chown calls the rule leaves alone: production code, which a test
// does not run as another user, and the guarded forms in its test file.
package guardedchown

import "os"

// Give hands path to uid: production code is not judged by a rule on tests.
func Give(path string, uid int) error { return os.Chown(path, uid, -1) }
