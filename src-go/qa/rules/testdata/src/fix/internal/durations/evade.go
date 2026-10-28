package durations

import (
	stdtime "time"
)

// heldInVariable converts first and multiplies later.
func heldInVariable(n int) stdtime.Duration {
	d := stdtime.Duration(n)
	return d * stdtime.Hour // want `time.Duration from a number with no bound`
}

// inPlace multiplies a duration by a converted number in place.
func inPlace(n int) stdtime.Duration {
	d := stdtime.Second
	d *= stdtime.Duration(n) // want `time.Duration from a number with no bound`
	return d
}

// parenthesised hides the conversion in parentheses and an offset.
func parenthesised(n int) stdtime.Duration {
	return ((stdtime.Duration(n - 1))) * (24 * stdtime.Hour) // want `time.Duration from a number with no bound`
}

// fromFloat converts seconds as a float: a large one is out of int64's range.
func fromFloat(f float64) stdtime.Duration {
	return stdtime.Duration(f * float64(stdtime.Second)) // want `time.Duration from a number with no bound`
}

// boundTooLarge is bounded, but by a constant whose product with the unit leaves int64.
func boundTooLarge(days int) stdtime.Duration {
	if days > 1_000_000 {
		return 0
	}
	return stdtime.Duration(days) * 24 * stdtime.Hour // want `bounded by 1000000, which leaves int64`
}

// twoNumbers multiplies two converted numbers, one of them unbounded.
func twoNumbers(a, b int) stdtime.Duration {
	if a > 60 {
		return 0
	}
	return stdtime.Duration(a) * stdtime.Duration(b) // want `time.Duration from a number with no bound`
}

// boundOnAnotherVariable is a bound that is not on the number converted.
func boundOnAnotherVariable(n, m int) stdtime.Duration {
	if m > 60 {
		return 0
	}
	return stdtime.Duration(n) * stdtime.Second // want `time.Duration from a number with no bound`
}
