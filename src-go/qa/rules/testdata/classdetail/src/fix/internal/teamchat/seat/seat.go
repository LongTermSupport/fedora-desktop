// Package seat holds the notice record, as teamchat's seat package has it.
package seat

import "fix/internal/teamchat/matrix"

// NoticeRecord is held/notice.json.
type NoticeRecord struct {
	V   int
	Why string
}

const whyRetried = "; retried"

func pending(err error) NoticeRecord {
	return NoticeRecord{V: 1, Why: matrix.Class(err) + whyRetried}
}

func positional(err error) NoticeRecord { return NoticeRecord{1, matrix.Class(err)} }

func pointer(err error) *NoticeRecord { return &NoticeRecord{Why: (matrix.Class(err))} }

// A class put in a variable first leaves the record's literal: it is judged as a line.
func viaVariable(err error) NoticeRecord {
	why := matrix.Class(err) // want `a failed call's class printed without its detail`
	return NoticeRecord{Why: why}
}
