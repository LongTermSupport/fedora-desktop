package businstall

import (
	"errors"
	"fmt"
	"io/fs"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/firewalld"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
)

const recordName = "firewalld.rules"

// fsRecord is the firewalld record, a 0600 file in root's own directory for the team.
// Writing it is not a change to report: it is the installer's bookkeeping.
type fsRecord struct {
	dir *fsx.FS
	own fsx.Owner
}

func (r fsRecord) Read() ([]byte, bool, error) {
	data, err := r.dir.ReadFile(recordName)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return data, true, nil
}

func (r fsRecord) Write(data []byte) error {
	_, err := r.dir.Quiet().PutBytes("firewalld record", data, recordName, privateFile, r.own)
	return err
}

func (r fsRecord) Delete() error {
	_, err := r.dir.RemoveIfPresent(recordName)
	return err
}

// noRecord is the record of a team with no install state: nothing was ever recorded.
type noRecord struct{}

func (noRecord) Read() ([]byte, bool, error) { return nil, false, nil }

func (noRecord) Write([]byte) error {
	return errors.New("there is no install state to keep the firewalld record in")
}

func (noRecord) Delete() error { return nil }

// fwRecord is the team's firewalld record.
func (a *App) fwRecord(team string) (firewalld.Record, error) {
	dir, found, err := a.tryFS(installDir + "/" + team)
	if err != nil {
		return nil, fmt.Errorf("the firewalld record: %w", err)
	}
	if !found {
		return noRecord{}, nil
	}
	root, err := a.rootOwner()
	if err != nil {
		return nil, err
	}
	return fsRecord{dir: dir, own: root}, nil
}
