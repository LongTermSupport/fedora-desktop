// Package v1 is the export of a v1 agent-team's room history before v1 is retired: `teamchat-admin
// migrate export` and `migrate export-verify` (Plan 00178, SPEC-v1-export.md). It is the one v2
// package that names v1 (its state paths, its service user, its event types: Plan 00209 D 4.1,
// the gate teamchat-keeps-off-v1), and it reads v1 only: files by root, the homeserver by GET on
// loopback.
//
// Expectations it implements:
//
//   - E1 the team name grammar (CheckTeam) and the copy's two fields together (Export);
//   - E2 every file is opened by the caller, root, which the verb class decides;
//   - E3-E6 the team's directory, team.json, the room ids and the admin token, each checked for
//     kind, owner and mode (readTeam); the token reaches only an Authorization header;
//   - E7 GET only, to 127.0.0.1:<port>, through the injected client (production:
//     matrix.LoopbackClient, no proxy, no redirect, RequestLimit a request);
//   - E8 no answer refused with the way on; E9 whoami answering @admin:<server_name>;
//   - E10 paging each room backwards from a sync token, with no type filter and no page cap,
//     then chronological; E11 each room's state;
//   - E12 the record directory, built beside its final name and renamed into place with no
//     replace; E13 events-<purpose>.jsonl; E14 state-<purpose>.json; E15 history.md
//     (renderHistory); E16 digest.json, written last;
//   - E17 the human copy, verified before success; E18 Verify; E19 the result and progress;
//   - E20 is the verbs' (package admin); E21 one stamped record per export.
package v1

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// RequestLimit bounds each request to the homeserver (E7).
const RequestLimit = 30 * time.Second

// Account is a local account's ids.
type Account struct {
	UID, GID int
}

// Deps is what an export reaches: Root prefixes every path ("" on a host); RootUID is the owner
// of root's own files (0 on a host); Lookup finds a local account by name (found false when there
// is none); HTTP sends each GET; Now is the clock the stamp and the export time are read from;
// Chown gives an open file of the human copy to its owner ((*os.File).Chown on a host).
type Deps struct {
	Root    string
	RootUID int
	Lookup  func(name string) (Account, bool, error)
	HTTP    Doer
	Now     func() time.Time
	Chown   func(f *os.File, uid, gid int) error
}

// Request is one export: the v1 team, and, both or neither, the directory the human copy goes in
// and the account that owns it. Progress takes the diagnostics (stderr).
type Request struct {
	Team      string
	CopyTo    string
	CopyOwner string
	Progress  io.Writer
}

// Result is a finished export: the record's path, the copy's ("" for none) and each room read.
type Result struct {
	Record string
	Copy   string
	Rooms  []RoomCount
}

// RoomCount is one room of a record: its purpose, its id and the number of its events.
type RoomCount struct {
	Purpose string
	RoomID  string
	Events  int
}

// Exporter runs exports and verifications over its Deps.
type Exporter struct {
	Deps Deps
}

// Export reads the v1 team's rooms and writes the record, then the human copy when asked.
func (x Exporter) Export(ctx context.Context, req Request) (Result, error) {
	d := x.Deps
	if err := CheckTeam(req.Team); err != nil {
		return Result{}, err
	}
	if (req.CopyTo == "") != (req.CopyOwner == "") {
		return Result{}, errors.New("--copy-to and --copy-owner come together or not at all")
	}
	say := func(format string, args ...any) error {
		if _, err := fmt.Fprintf(req.Progress, format+"\n", args...); err != nil {
			return fmt.Errorf("cannot write the progress: %w", err)
		}
		return nil
	}
	if req.CopyTo != "" {
		if _, err := copyTarget(d, req); err != nil {
			return Result{}, err
		}
	}
	t, err := readTeam(d, req.Team)
	if err != nil {
		return Result{}, err
	}
	now := d.Now().UTC()
	stamp := now.Format(stampLayout)
	if err := absent(recordPath(d, req.Team, stamp)); err != nil {
		return Result{}, err
	}
	rooms, err := readRooms(ctx, d, t, say)
	if err != nil {
		return Result{}, err
	}
	files, err := recordFiles(t, now, rooms)
	if err != nil {
		return Result{}, err
	}
	rec, err := writeRecord(d, t.name, stamp, files)
	if err != nil {
		return Result{}, err
	}
	res := Result{Record: rec}
	for _, r := range rooms {
		res.Rooms = append(res.Rooms, RoomCount{Purpose: r.purpose, RoomID: r.id, Events: len(r.events)})
	}
	if err := say("record %s written", rec); err != nil {
		return Result{}, err
	}
	if req.CopyTo != "" {
		if res.Copy, err = copyRecord(d, rec, t.name, stamp, req); err != nil {
			return Result{}, err
		}
		if err := say("copied to %s", res.Copy); err != nil {
			return Result{}, err
		}
	}
	return res, nil
}

// readRooms reads each room the team has, in purpose order (E9-E11): whoami first, then for each
// room a starting token, its events and its state.
func readRooms(ctx context.Context, d Deps, t team, say func(string, ...any) error) ([]roomRecord, error) {
	c := client{http: d.HTTP, port: t.port, token: t.token}
	if err := c.whoami(ctx, t.serverName); err != nil {
		if e, ok := errors.AsType[*noAnswerError](err); ok && e != nil {
			return nil, fmt.Errorf("the homeserver of v1 team %s at %s is not running (%w): a team declared absent is stopped "+
				"and blocked by v1's play, so declare it present again (its archive opened) before it is exported", t.name, c.base(), err)
		}
		return nil, err
	}
	var rooms []roomRecord
	for _, purpose := range purposes {
		id, ok := t.rooms[purpose]
		if !ok {
			continue
		}
		r, err := readRoom(ctx, c, purpose, id)
		if err != nil {
			return nil, fmt.Errorf("the %s room %s: %w; nothing is kept", purpose, id, err)
		}
		if err := say("%s %s: %d events", purpose, id, len(r.events)); err != nil {
			return nil, err
		}
		rooms = append(rooms, r)
	}
	return rooms, nil
}

func readRoom(ctx context.Context, c client, purpose, id string) (roomRecord, error) {
	from, err := c.startToken(ctx, id)
	if err != nil {
		return roomRecord{}, err
	}
	events, err := c.history(ctx, id, from)
	if err != nil {
		return roomRecord{}, err
	}
	state, err := c.state(ctx, id)
	if err != nil {
		return roomRecord{}, err
	}
	return roomRecord{purpose: purpose, id: id, events: events, state: state}, nil
}

// Verify checks the record of team named by stamp, or the newest when stamp is "" (E18). It
// returns the record's path; a *MismatchError lists each file that does not match.
func (x Exporter) Verify(teamName, stamp string) (string, error) {
	if err := CheckTeam(teamName); err != nil {
		return "", err
	}
	dir := teamRecords(x.Deps, teamName)
	if stamp == "" {
		var err error
		if stamp, err = newest(dir); err != nil {
			return "", fmt.Errorf("no export of v1 team %s: %w", teamName, err)
		}
	} else if err := CheckStamp(stamp); err != nil {
		return "", err
	}
	rec := dir + "/" + stamp
	if _, err := verifyDir(rec, teamName); err != nil {
		return rec, err
	}
	return rec, nil
}
