// Package proto is the fixture's wire parser: Parse is strict, ForeignObject is the tolerant
// decoder of a foreign producer's object, and DecodeThing wraps Parse.
package proto

import "errors"

// Value is one parsed JSON value.
type Value struct{ s string }

// Parse is the strict wire parser.
func Parse(data []byte) (Value, error) {
	if len(data) == 0 {
		return Value{}, errors.New("empty")
	}
	return Value{s: string(data)}, nil
}

// ForeignObject is the read keys of a foreign object, each parsed strictly, the rest skipped.
func ForeignObject(data []byte, keys ...string) (map[string]Value, error) {
	out := map[string]Value{}
	for _, k := range keys {
		v, err := Parse(data)
		if err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, nil
}

// DecodeThing wraps Parse, so its argument is parsed strictly.
func DecodeThing(raw []byte) (Value, error) { // want DecodeThing:"strictly parses parameters 0"
	return Parse(raw)
}

// relay hands its argument on to Parse two calls down.
func relay(s string) (Value, error) { // want relay:"strictly parses parameters 0"
	b := []byte(s)
	return DecodeThing(b)
}

// DecodeString parses a string strictly through relay.
func DecodeString(s string) (Value, error) { return relay(s) } // want DecodeString:"strictly parses parameters 0"
