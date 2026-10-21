package rclonecache

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
)

// stats is what the report reads of a mount's vfs/stats (E13).
type stats struct {
	used, files, queued, inProgress, errored int64
	outOfSpace                               bool
	max, maxAgeS                             int64
}

type vfsStats struct {
	DiskCache *struct {
		BytesUsed         json.Number `json:"bytesUsed"`
		Files             json.Number `json:"files"`
		UploadsQueued     json.Number `json:"uploadsQueued"`
		UploadsInProgress json.Number `json:"uploadsInProgress"`
		ErroredFiles      json.Number `json:"erroredFiles"`
		OutOfSpace        bool        `json:"outOfSpace"`
	} `json:"diskCache"`
	Opt *struct {
		CacheMaxSize json.Number `json:"CacheMaxSize"`
		CacheMaxAge  json.Number `json:"CacheMaxAge"`
	} `json:"opt"`
}

// parseStats reads one JSON object; a missing or null counter is 0, a float is truncated.
func parseStats(data []byte) (stats, error) {
	var v vfsStats
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(&v); err != nil {
		return stats{}, err
	}
	if dec.More() {
		return stats{}, errors.New("more than one JSON value")
	}
	var s stats
	var ageNs int64
	type counter struct {
		n   json.Number
		dst *int64
	}
	var nums []counter
	if dc := v.DiskCache; dc != nil {
		nums = append(nums, counter{dc.BytesUsed, &s.used}, counter{dc.Files, &s.files},
			counter{dc.UploadsQueued, &s.queued}, counter{dc.UploadsInProgress, &s.inProgress},
			counter{dc.ErroredFiles, &s.errored})
		s.outOfSpace = dc.OutOfSpace
	}
	if o := v.Opt; o != nil {
		nums = append(nums, counter{o.CacheMaxSize, &s.max}, counter{o.CacheMaxAge, &ageNs})
	}
	for _, x := range nums {
		n, err := integer(x.n)
		if err != nil {
			return stats{}, err
		}
		*x.dst = n
	}
	s.maxAgeS = ageNs / 1_000_000_000
	return s, nil
}

// integer is a JSON number as an int64, a float truncated; the empty number (null) is 0.
func integer(n json.Number) (int64, error) {
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
