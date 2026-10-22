package state

import (
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/member/lock"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// MaxLockRecord bounds the seat lock's record (B 2.5).
const MaxLockRecord = 4 << 10

const lockRecordFile = "seat.lock"

// LockRecord is B 2.5's seat lock record: the run that holds the seat, when it started, and,
// after a launch failure, when it failed (LK4). The host reads it only for TB2.1's bounded
// effect, the run id by its grammar.
type LockRecord struct {
	V              int         `json:"v"`
	Run            paths.RunID `json:"run"`
	StartedMS      int64       `json:"started_ms"`
	LaunchFailedMS *int64      `json:"launch_failed_ms,omitempty"`
}

// LaunchFailed is whether the keeper recorded a launch failure (B 2.4.1 step 7).
func (r LockRecord) LaunchFailed() bool { return r.LaunchFailedMS != nil }

// Encode is the record's bytes.
func (r LockRecord) Encode() ([]byte, error) {
	r.V = Version
	return encode(lockRecordFile, r, DecodeLockRecord)
}

// DecodeLockRecord reads a seat lock record strictly; the health tick and the owner's seat
// sweep read a member's by the same rule.
func DecodeLockRecord(data []byte) (LockRecord, error) {
	var r LockRecord
	_, err := decodeStrict(lockRecordFile, data, MaxLockRecord, &r, func(c *rules) {
		c.need(validMS(r.StartedMS), "started_ms is not a time")
		c.need(r.LaunchFailedMS == nil || validMS(*r.LaunchFailedMS), "launch_failed_ms is not a time")
	})
	if err != nil {
		return LockRecord{}, err
	}
	return r, nil
}

// WriteLockRecord writes r into the seat lock through its held description (LK4).
func WriteLockRecord(l *lock.SeatLock, r LockRecord) error {
	data, err := r.Encode()
	if err != nil {
		return err
	}
	return l.WriteRecord(data)
}

// validMS is a millisecond time A allows: 0 to proto.MaxSentMs.
func validMS(ms int64) bool { return ms >= 0 && ms <= proto.MaxSentMs }
