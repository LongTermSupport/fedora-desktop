// Package sharedlimits stands for tlsterm: a connection's handshake and its first request are each
// bounded by 10 s, armed one after the other on the one fake clock, so a test that names 10 s can
// be answered by the handshake's bound while it means the request's.
package sharedlimits

import "time"

const (
	// HandshakeLimit bounds the handshake.
	HandshakeLimit = 10 * time.Second
	// RequestLimit bounds the first request, armed once the handshake has ended.
	RequestLimit = 10 * time.Second
	// IdleLimit is a bound no other limit shares.
	IdleLimit = 2 * time.Minute
)

// NotADuration has the value of the shared limits but is no Duration: it is not counted, so
// IdleLimit's value stays one limit's alone in TestClean's view of an int64 twin.
const NotADuration = int64(2 * time.Minute)
