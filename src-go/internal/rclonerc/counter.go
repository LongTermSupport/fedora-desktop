package rclonerc

import (
	"encoding/json"
	"fmt"
	"math"
)

// Counter is an RC answer's counter as an int64: a float is truncated, the empty number (a JSON
// null, or a field that is absent) is 0, and a value an int64 cannot hold is refused.
func Counter(n json.Number) (int64, error) {
	if n == "" {
		return 0, nil
	}
	if i, err := n.Int64(); err == nil {
		return i, nil
	}
	f, err := n.Float64()
	if err != nil || f >= math.MaxInt64 || f <= math.MinInt64 {
		return 0, fmt.Errorf("not a counter: %s", n)
	}
	return int64(f), nil
}
