package paths

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// carryNames is every name of carry.go, for each sample team; sampleNames includes it, so the v1
// and derivation checks cover these names too.
func carryNames(t *testing.T) map[string][]string {
	t.Helper()
	names := map[string][]string{"CarryDir": {CarryDir}, "CarryMaxArchiveVar": {CarryMaxArchiveVar}, "CarryMaxArchiveFile": {CarryMaxArchiveFile}}
	for _, s := range sampleTeams {
		team := mustTeam(t, s)
		for name, values := range map[string][]string{
			"SealRecord":             {SealRecord(team)},
			"DrillRecord":            {DrillRecord(team)},
			"CarriedRecord":          {CarriedRecord(team)},
			"CarryExport":            {CarryExport(team)},
			"CarryDrillExport":       {CarryDrillExport(team)},
			"CarryPart":              {CarryPart(team, false), CarryPart(team, true)},
			"CarryImportRecord":      {CarryImportRecord(team)},
			"CarryStaging":           {CarryStaging(team)},
			"HomeserverCarryStaging": {HomeserverCarryStaging(team)},
			"TeamStateRoots":         TeamStateRoots(team),
		} {
			names[name] = append(names[name], values...)
		}
	}
	return names
}

// carryZeroDerivations is carry.go's functions of a zero team id, which zeroDerivations
// includes.
func carryZeroDerivations() map[string]func() string {
	var team TeamID
	return map[string]func() string{
		"SealRecord":             func() string { return SealRecord(team) },
		"DrillRecord":            func() string { return DrillRecord(team) },
		"CarriedRecord":          func() string { return CarriedRecord(team) },
		"CarryExport":            func() string { return CarryExport(team) },
		"CarryDrillExport":       func() string { return CarryDrillExport(team) },
		"CarryPart":              func() string { return CarryPart(team, true) },
		"CarryImportRecord":      func() string { return CarryImportRecord(team) },
		"CarryStaging":           func() string { return CarryStaging(team) },
		"HomeserverCarryStaging": func() string { return HomeserverCarryStaging(team) },
		"TeamStateRoots":         func() string { return strings.Join(TeamStateRoots(team), " ") },
	}
}

// CN1-CN3: the carry's records, inbox and staging names.
func TestCarryNames(t *testing.T) {
	team := mustTeam(t, "ops")
	for got, want := range map[string]string{
		SealRecord(team):             "/var/lib/teamchat-admin/ops/sealed.json",
		DrillRecord(team):            "/var/lib/teamchat-admin/ops/drill.json",
		CarriedRecord(team):          "/var/lib/teamchat-admin/ops/carried.json",
		CarryDir:                     "/var/lib/teamchat-admin/_carry",
		CarryExport(team):            "/var/lib/teamchat-admin/_carry/ops.tar",
		CarryDrillExport(team):       "/var/lib/teamchat-admin/_carry/ops.drill.tar",
		CarryPart(team, false):       "/var/lib/teamchat-admin/_carry/ops.tar.part",
		CarryPart(team, true):        "/var/lib/teamchat-admin/_carry/ops.drill.tar.part",
		CarryImportRecord(team):      "/var/lib/teamchat-admin/_carry/ops.import.json",
		CarryStaging(team):           "/var/lib/teamchat-admin/_carry/ops.staging",
		HomeserverCarryStaging(team): "/var/lib/private/teamchat/.carry-ops",
		CarryMaxArchiveVar:           "teamchat_carry_max_archive_bytes",
		CarryMaxArchiveFile:          "/etc/teamchat/carry-max-archive-bytes",
	} {
		if got != want {
			t.Errorf("got %s, want %s", got, want)
		}
	}
}

// CN2, CN3: neither carry directory can be read as a team's: each fails the team id grammar.
func TestTheCarryNamesAreNoTeamID(t *testing.T) {
	team := mustTeam(t, "ops")
	for _, name := range []string{filepath.Base(CarryDir), filepath.Base(HomeserverCarryStaging(team))} {
		if _, err := ParseTeamID(name); err == nil {
			t.Errorf("%s parses as a team id", name)
		}
	}
}

// CN4: TeamStateRoots is the closed list the spec gives, in its order.
func TestTeamStateRoots(t *testing.T) {
	team := mustTeam(t, "ops")
	want := []string{
		"/var/lib/teamchat-admin/ops",
		"/var/lib/teamchat-ctl/ops",
		"/var/lib/teamchat/ops",
		"/var/lib/private/teamchat/ops",
		"/var/lib/teamchat-tls/ops",
		"/var/lib/teamchat-status/teams/ops",
		"/var/lib/teamchat-status/lxc/ops.json",
		"/etc/systemd/system/teamchat-server@ops.service.d",
		"/etc/systemd/system/teamchat-tls@ops.service.d",
		"/var/lib/private/teamchat/.carry-ops",
		"/var/lib/teamchat-admin/_carry/ops.staging",
		"/var/lib/teamchat-admin/_carry/ops.import.json",
	}
	if got := TeamStateRoots(team); !slices.Equal(got, want) {
		t.Errorf("TeamStateRoots:\n got %q\nwant %q", got, want)
	}
}

// teamFuncs is every exported func(TeamID) string of the package; TestTeamFuncsAreEveryOne holds
// it complete against the source.
func teamFuncs() map[string]func(TeamID) string {
	return map[string]func(TeamID) string{
		"HomeserverUser": HomeserverUser, "ServerName": ServerName, "ServerUnit": ServerUnit,
		"ServerUnitDropIn": ServerUnitDropIn, "ServerRuntimeDirectory": ServerRuntimeDirectory,
		"FenceUnit": FenceUnit, "TLSUnit": TLSUnit, "TLSUnitDropIn": TLSUnitDropIn, "FenceTable": FenceTable,
		"HomeserverState": HomeserverState, "HomeserverPrivateState": HomeserverPrivateState,
		"AdminState": AdminState, "ServerConfig": ServerConfig, "TeamDeclaration": TeamDeclaration,
		"RestoreBlock": RestoreBlock, "MigrationDir": MigrationDir, "FenceFile": FenceFile,
		"FirewallRecord": FirewallRecord, "CADir": CADir, "CACertFile": CACertFile, "CAKeyFile": CAKeyFile,
		"TLSDir": TLSDir, "LeafCertFile": LeafCertFile, "LeafKeyFile": LeafKeyFile, "CtlState": CtlState,
		"RosterFile": RosterFile, "RoomsFile": RoomsFile, "GuestTokenDir": GuestTokenDir, "TeamLock": TeamLock,
		"StatusTeamDir": StatusTeamDir, "StampFile": StampFile, "StatusFile": StatusFile,
		"ApprovedFile": ApprovedFile, "LXCOnlineFile": LXCOnlineFile, "LXCRecord": LXCRecord,
		"ElementProfile": ElementProfile, "UnitsRecord": UnitsRecord, "ServerNameFile": ServerNameFile,
		"ServerDropInFile": ServerDropInFile, "TLSDropInFile": TLSDropInFile,
		"HomeserverDatabase": HomeserverDatabase, "HomeserverBackups": HomeserverBackups,
		"HomeserverPrivateDatabase": HomeserverPrivateDatabase, "RestoreRecord": RestoreRecord,
		"RestoreEpochFile": RestoreEpochFile, "RestoreConfig": RestoreConfig, "BackupDir": BackupDir,
		"BackupLastRun": BackupLastRun, "HomeserverPrivateBackups": HomeserverPrivateBackups,
		"HomeserverPrivateBackupMeta": HomeserverPrivateBackupMeta,
		"HomeserverPreviousDatabase":  HomeserverPreviousDatabase, "RestoreUnit": RestoreUnit,
		"RestoreCtlUnit": RestoreCtlUnit, "RestoreUnitCredential": RestoreUnitCredential,
		"SealRecord": SealRecord, "DrillRecord": DrillRecord, "CarriedRecord": CarriedRecord,
		"CarryExport": CarryExport, "CarryDrillExport": CarryDrillExport,
		"CarryImportRecord": CarryImportRecord, "CarryStaging": CarryStaging,
		"HomeserverCarryStaging": HomeserverCarryStaging, "RestoreDrillMarker": RestoreDrillMarker,
	}
}

// No two func(TeamID) string give one team the same path: two meanings of one file make each
// owner's reader take the other's file for its own (the restore drill's marker once shared the
// carry's drill record, so the carry guard refused the drill's own team, issue #324). Names that
// are not paths (a user, an Element profile) live in namespaces of their own and are not compared.
func TestNoTwoTeamFuncsNameTheSamePath(t *testing.T) {
	for _, s := range sampleTeams {
		if got := sharedTeamPaths(teamFuncs(), mustTeam(t, s)); len(got) != 0 {
			t.Errorf("team %s: %q name the same path: give each its own name", s, got)
		}
	}
}

// The check can fail: two functions that give the team one path are reported, by name.
func TestTheSharedPathCheckDiscriminates(t *testing.T) {
	funcs := map[string]func(TeamID) string{
		"DrillRecord": DrillRecord,
		"Twin":        func(t TeamID) string { return AdminState(t) + "/drill.json" },
		"RosterFile":  RosterFile,
	}
	if got := sharedTeamPaths(funcs, mustTeam(t, "ops")); !slices.Equal(got, []string{"DrillRecord=Twin"}) {
		t.Errorf("got %q, want [DrillRecord=Twin]", got)
	}
}

// sharedTeamPaths is each pair of funcs whose results for team are the same absolute path,
// "A=B" with A before B, sorted.
func sharedTeamPaths(funcs map[string]func(TeamID) string, team TeamID) []string {
	byValue := map[string][]string{}
	for name, f := range funcs {
		if v := f(team); strings.HasPrefix(v, "/") {
			byValue[v] = append(byValue[v], name)
		}
	}
	var out []string
	for _, names := range byValue {
		slices.Sort(names)
		for i := range names {
			for _, other := range names[i+1:] {
				out = append(out, names[i]+"="+other)
			}
		}
	}
	slices.Sort(out)
	return out
}

// notStateOfTheTeam are the func(TeamID) string whose result holds no state of the team on this
// host, so TeamStateRoots rightly leaves them out: unit, user, table and Element names (no path),
// the lock in /run that is never unlinked, the unit's credential in /run, and the carry inbox,
// which is the import's input and never removed by it (SI1, SI10).
var notStateOfTheTeam = []string{
	"HomeserverUser", "ServerName", "ServerUnit", "ServerUnitDropIn", "ServerRuntimeDirectory",
	"FenceUnit", "TLSUnit", "TLSUnitDropIn", "FenceTable", "ElementProfile", "RestoreUnit",
	"RestoreCtlUnit", "TeamLock", "RestoreUnitCredential", "CarryExport", "CarryDrillExport",
}

// CN4's completeness: every func(TeamID) string names a path under a root of TeamStateRoots, or
// is named in notStateOfTheTeam, so a new per-team path must join the list or the exemptions.
func TestTeamStateRootsAreComplete(t *testing.T) {
	for _, s := range sampleTeams {
		team := mustTeam(t, s)
		if got := uncoveredTeamPaths(teamFuncs(), TeamStateRoots(team), team); len(got) != 0 {
			t.Errorf("team %s: %q are paths of the team under no root of TeamStateRoots: add the root, or name the function in notStateOfTheTeam", s, got)
		}
	}
}

// The completeness check can fail: a per-team path under no root is reported, by name.
func TestTheStateRootCheckDiscriminates(t *testing.T) {
	team := mustTeam(t, "ops")
	funcs := map[string]func(TeamID) string{
		"Stray":      func(t TeamID) string { return "/var/lib/teamchat-stray/" + t.String() },
		"RosterFile": RosterFile,
	}
	if got := uncoveredTeamPaths(funcs, TeamStateRoots(team), team); !slices.Equal(got, []string{"Stray"}) {
		t.Errorf("got %q, want [Stray]", got)
	}
	if got := uncoveredTeamPaths(map[string]func(TeamID) string{"RosterFile": RosterFile}, nil, team); !slices.Equal(got, []string{"RosterFile"}) {
		t.Errorf("no roots: got %q, want [RosterFile]", got)
	}
}

// uncoveredTeamPaths is the names of funcs, outside notStateOfTheTeam, whose result for team is
// neither a root nor under one, sorted.
func uncoveredTeamPaths(funcs map[string]func(TeamID) string, roots []string, team TeamID) []string {
	var out []string
	for name, f := range funcs {
		if slices.Contains(notStateOfTheTeam, name) {
			continue
		}
		value := f(team)
		if !slices.ContainsFunc(roots, func(root string) bool { return value == root || strings.HasPrefix(value, root+"/") }) {
			out = append(out, name)
		}
	}
	slices.Sort(out)
	return out
}

// teamFuncs is every exported func(TeamID) string the source declares, and no other.
func TestTeamFuncsAreEveryOne(t *testing.T) {
	declared := teamFuncDecls(t)
	listed := teamFuncs()
	for _, name := range declared {
		if _, ok := listed[name]; !ok {
			t.Errorf("%s is a func(TeamID) string not in teamFuncs: add it", name)
		}
	}
	for name := range listed {
		if !slices.Contains(declared, name) {
			t.Errorf("teamFuncs has %s, which the package does not declare as func(TeamID) string", name)
		}
	}
	for _, name := range notStateOfTheTeam {
		if _, ok := listed[name]; !ok {
			t.Errorf("notStateOfTheTeam names %s, which is not a func(TeamID) string", name)
		}
	}
}

// teamFuncDecls lists the exported functions (not methods) whose one parameter is a TeamID and
// whose one result is a string.
func teamFuncDecls(t *testing.T) []string {
	t.Helper()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	fset := token.NewFileSet()
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		if !e.Type().IsRegular() {
			t.Fatalf("%s is not a regular file", e.Name())
		}
		file, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.IsExported() && isTeamFunc(fn.Type) {
				names = append(names, fn.Name.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("no func(TeamID) string found: the parse read nothing")
	}
	return names
}

func isTeamFunc(ft *ast.FuncType) bool {
	params, results := ft.Params.List, ft.Results
	if len(params) != 1 || len(params[0].Names) > 1 || results == nil || len(results.List) != 1 || len(results.List[0].Names) > 1 {
		return false
	}
	p, pok := params[0].Type.(*ast.Ident)
	r, rok := results.List[0].Type.(*ast.Ident)
	return pok && rok && p.Name == "TeamID" && r.Name == "string"
}
