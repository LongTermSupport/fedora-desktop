package restore

import (
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/proto"
)

// MaxEpoch is A 5.1's highest restore_epoch; a merge that would pass it refuses (n2g-D20).
const MaxEpoch = 999_999_999

// maxEpochFloor caps restore-epoch.json.
const maxEpochFloor = 4 << 10

// EpochFloor is root/<team>/restore-epoch.json (n2g-D20): the highest epoch any restore put in
// place (written at RS6's step 1), and the lowest backup a fresh restore may use (RS8's step 2,
// after a --roster-from-backup restore). Both are 0 when the file is absent.
type EpochFloor struct {
	Epoch, MinBackup int64
}

// Raised is each field the larger of f's and o's.
func (f EpochFloor) Raised(o EpochFloor) EpochFloor {
	return EpochFloor{Epoch: max(f.Epoch, o.Epoch), MinBackup: max(f.MinBackup, o.MinBackup)}
}

// EncodeEpochFloor is the file's JSON.
func EncodeEpochFloor(f EpochFloor) []byte {
	return proto.Obj(proto.M("v", proto.Int(1)), proto.M("epoch", proto.Int(f.Epoch)), proto.M("min_backup", proto.Int(f.MinBackup))).Compact()
}

// DecodeEpochFloor reads the file strictly.
func DecodeEpochFloor(raw []byte) (EpochFloor, error) {
	v, err := parse(raw, "restore-epoch.json", maxEpochFloor)
	if err != nil {
		return EpochFloor{}, err
	}
	rd := &reader{file: "restore-epoch.json"}
	m := rd.object(v, "file", []string{"v", "epoch", "min_backup"})
	rd.integer(m["v"], "v", 1, 1)
	f := EpochFloor{Epoch: rd.integer(m["epoch"], "epoch", 0, MaxEpoch), MinBackup: rd.integer(m["min_backup"], "min_backup", 0, maxBackup)}
	return f, rd.err
}
