package paths

import "path/filepath"

// The names of the sealed carry of a team: its seal, drill and import records, the root-only
// inbox an export is written to and an import reads from, the import's staging names, and the
// closed list of every root that holds a team's state (Plan 00209 SPEC-carry CN1-CN5).

// SealRecord is the team's seal (SC3, SC7); while it exists every verb but `team remove`,
// `team export` and `team seal` refuses.
func SealRecord(t TeamID) string { return AdminState(t) + "/sealed.json" }

// DrillRecord is a drill export under way (SD2).
func DrillRecord(t TeamID) string { return AdminState(t) + "/drill.json" }

// CarriedRecord is the record an import leaves in the team's admin state (SI9 step 4).
func CarriedRecord(t TeamID) string { return AdminState(t) + "/carried.json" }

// CarryDir is the root-only inbox of carry archives and the import's records and staging. Its
// name starts with an underscore, which no team id can, so a walk of AdminStateRoot that parses
// team ids skips it (CN2).
const CarryDir = AdminStateRoot + "/_carry"

// CarryExport is the team's sealed export, the one file `team import` reads (SX1, SI1).
func CarryExport(t TeamID) string { return CarryDir + "/" + t.text() + ".tar" }

// CarryDrillExport is the team's drill export (SD3).
func CarryDrillExport(t TeamID) string { return CarryDir + "/" + t.text() + ".drill.tar" }

// CarryPart is the export being written, renamed to CarryExport or CarryDrillExport once whole.
func CarryPart(t TeamID, drill bool) string {
	if drill {
		return CarryDrillExport(t) + ".part"
	}
	return CarryExport(t) + ".part"
}

// CarryImportRecord is the record of an import under way (SI8).
func CarryImportRecord(t TeamID) string { return CarryDir + "/" + t.text() + ".import.json" }

// CarryStaging is where an import stages the archive's admin, ctl and status members (SI8).
func CarryStaging(t TeamID) string { return CarryDir + "/" + t.text() + ".staging" }

// HomeserverCarryStaging is where an import stages the archive's homeserver tree: beside the
// team's HomeserverPrivateState, on the same file system, so its placement is one rename. Its
// leading dot fails the team id grammar (CN3).
func HomeserverCarryStaging(t TeamID) string {
	return HomeserverPrivateStateRoot + "/.carry-" + t.text()
}

// CarryMaxArchiveVar is the inventory variable of the carry archive's cap in bytes (CN5).
const CarryMaxArchiveVar = "teamchat_carry_max_archive_bytes"

// CarryMaxArchiveFile is the cap as the play writes it into teamchat-admin's configuration,
// which both export and import read (CN5).
const CarryMaxArchiveFile = EtcDir + "/carry-max-archive-bytes"

// TeamStateRoots is every path that holds state of the team on a host, the one list an import's
// empty-guest check, the lab's purge check and the tests use (CN4). The team lock in /run is not
// state: it is never unlinked.
func TeamStateRoots(t TeamID) []string {
	return []string{
		AdminState(t),
		CtlState(t),
		HomeserverState(t),
		HomeserverPrivateState(t),
		TLSDir(t),
		StatusTeamDir(t),
		LXCRecord(t),
		filepath.Dir(ServerDropInFile(t)),
		filepath.Dir(TLSDropInFile(t)),
		HomeserverCarryStaging(t),
		CarryStaging(t),
		CarryImportRecord(t),
	}
}
