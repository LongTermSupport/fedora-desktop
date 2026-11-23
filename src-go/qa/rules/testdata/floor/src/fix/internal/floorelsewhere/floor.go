// Package floorelsewhere is outside teamchat: a floor here is no event stream's, and is not judged.
package floorelsewhere

// Above is a number compared with a floor.
func Above(n, floor int64) bool { return n > floor }

// After is a time compared with a since.
func After(ts, since int64) bool { return ts <= since }
