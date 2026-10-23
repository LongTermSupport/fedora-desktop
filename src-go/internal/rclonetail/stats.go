package rclonetail

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/rclonerc"
)

// queue is what a tick reads of vfs/stats (E9).
type queue struct {
	inProgress, queued, errored int64
}

// transfers is what a tick reads of core/stats (E9); hasETA is false for a missing or null eta.
type transfers struct {
	bytes, totalBytes, done, total, eta int64
	hasETA                              bool
}

// decodeOne reads exactly one JSON value into v, numbers kept as written.
func decodeOne(data []byte, v any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("more than one JSON value")
	}
	return nil
}

// counters fills each destination from its number (rclonerc.Counter).
func counters(pairs map[*int64]json.Number) error {
	for dst, n := range pairs {
		v, err := rclonerc.Counter(n)
		if err != nil {
			return err
		}
		*dst = v
	}
	return nil
}

func parseQueue(data []byte) (queue, error) {
	var v struct {
		DiskCache *struct {
			UploadsInProgress json.Number `json:"uploadsInProgress"`
			UploadsQueued     json.Number `json:"uploadsQueued"`
			ErroredFiles      json.Number `json:"erroredFiles"`
		} `json:"diskCache"`
	}
	if err := decodeOne(data, &v); err != nil {
		return queue{}, err
	}
	var q queue
	if dc := v.DiskCache; dc != nil {
		if err := counters(map[*int64]json.Number{
			&q.inProgress: dc.UploadsInProgress, &q.queued: dc.UploadsQueued, &q.errored: dc.ErroredFiles,
		}); err != nil {
			return queue{}, err
		}
	}
	return q, nil
}

func parseTransfers(data []byte) (transfers, error) {
	var v struct {
		Bytes          json.Number `json:"bytes"`
		TotalBytes     json.Number `json:"totalBytes"`
		Transfers      json.Number `json:"transfers"`
		TotalTransfers json.Number `json:"totalTransfers"`
		ETA            json.Number `json:"eta"`
	}
	if err := decodeOne(data, &v); err != nil {
		return transfers{}, err
	}
	var t transfers
	if err := counters(map[*int64]json.Number{
		&t.bytes: v.Bytes, &t.totalBytes: v.TotalBytes, &t.done: v.Transfers, &t.total: v.TotalTransfers, &t.eta: v.ETA,
	}); err != nil {
		return transfers{}, err
	}
	t.hasETA = v.ETA != ""
	return t, nil
}

// eta is an estimate in seconds as minutes and seconds (E12); none, or 0 or less, is a dash.
func eta(s int64, has bool) string {
	if !has || s <= 0 {
		return "—"
	}
	return fmt.Sprintf("%dm%02ds", s/60, s%60)
}
