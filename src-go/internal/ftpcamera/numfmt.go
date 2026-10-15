package ftpcamera

import (
	"math"
	"strconv"
)

// iecUnits are the unit letters numfmt scales through.
const iecUnits = "KMGTPE"

const iecBase = 1024

// iec is n as `numfmt --to=iec` (binary false) or `--to=iec-i` (true) prints it (E65): under
// 1024 the whole number; otherwise scaled by 1024, one decimal below 10, rounded away from
// zero, moving up a unit when the rounding reaches 1024.
func iec(n int64, binary bool) string {
	if n < iecBase {
		return strconv.FormatInt(n, 10)
	}
	x := float64(n)
	unit := 0
	for x >= iecBase && unit < len(iecUnits) {
		x /= iecBase
		unit++
	}
	var text string
	if tenths := math.Ceil(x * 10); tenths < 100 {
		text = strconv.FormatFloat(tenths/10, 'f', 1, 64)
	} else {
		whole := math.Ceil(x)
		if whole >= iecBase && unit < len(iecUnits) {
			unit++
			text = "1.0"
		} else {
			text = strconv.FormatFloat(whole, 'f', 0, 64)
		}
	}
	suffix := string(iecUnits[unit-1])
	if binary {
		suffix += "i"
	}
	return text + suffix
}
