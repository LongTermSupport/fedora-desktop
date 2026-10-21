// Package notests has no _test.go file.
package notests // want `package has no _test.go file`

// F is untested code the coverage floor could hide behind the rest of the module.
func F() int { return 1 }
