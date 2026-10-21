// Package externaltests is tested only from an external test package.
package externaltests

// F is tested only from an external test package (package externaltests_test).
func F() int { return 1 }
