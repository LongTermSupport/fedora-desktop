package paths

import (
	"bufio"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"slices"
	"strings"
	"testing"
)

// v1NamesFile is the static gate's v1 name list (D 4.1), read here so the list has one home.
const v1NamesFile = "../../../qa/teamchatseparate/v1-names.txt"

// v1Name is one entry of the list: a name found in any case, or, with next set, a name found
// exactly as written wherever next does not follow it.
type v1Name struct{ name, next string }

func loadV1Names(t *testing.T) []v1Name {
	t.Helper()
	f, err := os.Open(v1NamesFile)
	if err != nil {
		t.Fatalf("open the v1 name list: %v", err)
	}
	defer func() {
		if cerr := f.Close(); cerr != nil {
			t.Errorf("close the v1 name list: %v", cerr)
		}
	}()
	var names []v1Name
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, next, exact := strings.Cut(line, " !")
		if !exact {
			next = ""
		}
		names = append(names, v1Name{name: name, next: next})
	}
	if err := sc.Err(); err != nil {
		t.Fatalf("read the v1 name list: %v", err)
	}
	if len(names) == 0 {
		t.Fatal("the v1 name list is empty: a check against nothing proves nothing")
	}
	return names
}

// holds reports whether s holds the v1 name, by the list's two rules.
func (v v1Name) holds(s string) bool {
	if v.next == "" {
		return strings.Contains(strings.ToLower(s), strings.ToLower(v.name))
	}
	for i := 0; ; {
		j := strings.Index(s[i:], v.name)
		if j < 0 {
			return false
		}
		end := i + j + len(v.name)
		if !strings.HasPrefix(s[end:], v.next) {
			return true
		}
		i = end
	}
}

// The checks below would pass over a list they cannot match: each entry must flag a value that
// holds it, and the exact form must pass the v2 name it is written to allow.
func TestV1NameMatcherDiscriminates(t *testing.T) {
	for _, v := range loadV1Names(t) {
		if !v.holds("/x/" + v.name + "z") {
			t.Errorf("%q does not flag a value holding it", v.name)
		}
		if v.next == "" {
			if !v.holds("/x/" + strings.ToUpper(v.name)) {
				t.Errorf("%q does not flag a value holding it in another case", v.name)
			}
			continue
		}
		if v.holds("/x/" + v.name + v.next + "ops") {
			t.Errorf("%q flags the v2 name with %q after it", v.name, v.next)
		}
		if v.holds("/x/" + strings.ToLower(v.name) + "ops") {
			t.Errorf("%q is exact-case, yet flags its lower case", v.name)
		}
	}
}

// sampleNames is every exported name of the package, expanded: a constant by its value, a
// function by its value for sample identifiers (the shortest, a typical and the longest team
// id). The completeness test below holds this table to the package's exported identifiers.
func sampleNames(t *testing.T) map[string][]string {
	t.Helper()
	names := constNames()
	for _, add := range []map[string][]string{idNames(t), baseNames(t), layoutNames(t), teamNames(t)} {
		for ident, values := range add {
			names[ident] = append(names[ident], values...)
		}
	}
	return names
}

// constNames is every exported constant by its value.
func constNames() map[string][]string {
	values := map[string]string{
		"Product": Product, "MatrixNamespace": MatrixNamespace, "MatrixPrefix": MatrixPrefix,
		"EnvPrefix": EnvPrefix, "EnvRuntime": EnvRuntime, "EnvKit": EnvKit,
		"EnvStatusLineInner": EnvStatusLineInner, "MemberBinary": MemberBinary,
		"AdminBinary": AdminBinary, "InstallBinary": InstallBinary,
		"MemberBinaryPath": MemberBinaryPath, "AdminBinaryPath": AdminBinaryPath,
		"PluginName": PluginName, "SkillName": SkillName, "TmuxSocket": TmuxSocket,
		"TmuxSession": TmuxSession, "CtlUser": CtlUser, "Interface": Interface,
		"LibDir": LibDir, "ShareDir": ShareDir, "EtcDir": EtcDir, "OwnerGroupFile": OwnerGroupFile,
		"ServerDir": ServerDir, "ServerPinFile": ServerPinFile, "HostKitRoot": HostKitRoot,
		"HostKitCurrent": HostKitCurrent, "HomeserverStateRoot": HomeserverStateRoot,
		"HomeserverPrivateStateRoot": HomeserverPrivateStateRoot, "AdminStateRoot": AdminStateRoot,
		"CtlStateRoot": CtlStateRoot, "StatusRoot": StatusRoot, "StatusInstalledFile": StatusInstalledFile,
		"AdminRunDir": AdminRunDir, "ServerUnitTemplate": ServerUnitTemplate,
		"FenceUnitTemplate": FenceUnitTemplate, "ServerStateDirectory": ServerStateDirectory,
		"HealthService": HealthService, "HealthTimer": HealthTimer, "BackupService": BackupService,
		"BackupTimer": BackupTimer, "SeatsService": SeatsService, "SeatsTimer": SeatsTimer,
		"SyslogServer": SyslogServer, "SyslogHealth": SyslogHealth, "SyslogBackup": SyslogBackup,
		"HostVarTeams": HostVarTeams, "HostVarAddress": HostVarAddress,
		"HostVarLXCMembers": HostVarLXCMembers, "HostVarElementTeams": HostVarElementTeams,
		"HostVarMigrations": HostVarMigrations, "OnlyTeamsVar": OnlyTeamsVar, "PlayFile": PlayFile,
		"ElementPlayFile": ElementPlayFile, "AnsibleTag": AnsibleTag, "TeamPortField": TeamPortField,
		"CCYKitDir": CCYKitDir, "SessionSeatMount": SessionSeatMount,
		"SessionMembershipMount": SessionMembershipMount, "CCYRunRoot": CCYRunRoot,
	}
	names := map[string][]string{}
	for ident, value := range values {
		names[ident] = []string{value}
	}
	return names
}

// idNames is every function of an identifier other than a team id, for a sample of each.
func idNames(t *testing.T) map[string][]string {
	t.Helper()
	set, user, version, digest := sampleSetID(t), sampleGuestUser(t), sampleServerVersion(t), sampleKitDigest(t)
	return map[string][]string{
		"SessionUnit":         {SessionUnit(user)},
		"ServerBinary":        {ServerBinary(version)},
		"HostKitDir":          {HostKitDir(digest)},
		"KitBinary":           {KitBinary(HostKitCurrent), KitBinary(CCYKitDir)},
		"KitPluginDir":        {KitPluginDir(HostKitCurrent)},
		"KitSettings":         {KitSettings(CCYKitDir)},
		"SetLock":             {SetLock(set)},
		"SetRecord":           {SetRecord(set)},
		"ElementLauncherName": {ElementLauncherName("Ops")},
	}
}

// baseNames is every function of a base directory, and the owner's seat store under one.
func baseNames(t *testing.T) map[string][]string {
	t.Helper()
	session, key := sampleSessionName(t), sampleCheckoutKey(t)
	store, err := SeatStore("/home/u/.local/state")
	if err != nil {
		t.Fatal(err)
	}
	stateHome, err := StateHome("/home/u", "")
	if err != nil {
		t.Fatal(err)
	}
	alloc, err := AllocLock("/run/user/1000", key)
	if err != nil {
		t.Fatal(err)
	}
	consent, err := ElementConsentFile("/home/u")
	if err != nil {
		t.Fatal(err)
	}
	return map[string][]string{
		"StateHome":          {stateHome},
		"AllocLock":          {alloc},
		"ElementConsentFile": {consent},
		"Store.Pool":         {store.Pool()},
		"Store.PoolCheckout": {store.PoolCheckout(key)},
		"Store.Session":      {store.Session(session)},
		"Store.Control":      {store.Control(session)},
		"Store.DesiredFile":  {store.DesiredFile(session)},
		"Store.Released":     {store.Released()},
	}
}

// layoutNames is both member layouts and a session runtime under each.
func layoutNames(t *testing.T) map[string][]string {
	t.Helper()
	home, err := HomeLayout("/home/u", "/run/user/1000")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string][]string{}
	for _, l := range []Layout{CCYLayout(), home} {
		r := l.Run(sampleRunID(t))
		for name, value := range map[string][]string{
			"Layout.DesiredFile":      {l.DesiredFile()},
			"Layout.ConversationFile": {l.ConversationFile()},
			"Layout fields":           {l.KitRoot, l.CurrentKit, l.State, l.Desired, l.RunRoot},
			"Run.KeeperLock":          {r.KeeperLock()},
			"Run.KeeperJSON":          {r.KeeperJSON()},
			"Run.KeeperLog":           {r.KeeperLog()},
			"Run.LinkSock":            {r.LinkSock()},
			"Run.LinkLock":            {r.LinkLock()},
			"Run.LinkLog":             {r.LinkLog()},
			"Run.Settings":            {r.Settings()},
			"Run.Session":             {r.Session()},
			"Run.Status":              {r.Status()},
		} {
			names[name] = append(names[name], value...)
		}
	}
	return names
}

// teamNames is every function of a team id, for the shortest, a typical and the longest id.
func teamNames(t *testing.T) map[string][]string {
	t.Helper()
	names := map[string][]string{}
	for _, s := range []string{"x", "ops", strings.Repeat("z", 22)} {
		team := mustTeam(t, s)
		dir, err := ElementConfigDir("/home/u", team)
		if err != nil {
			t.Fatal(err)
		}
		launcher, err := ElementLauncher("/home/u", team)
		if err != nil {
			t.Fatal(err)
		}
		seat := CCYLayout().Seat(team)
		for name, value := range map[string]string{
			"HomeserverUser":         HomeserverUser(team),
			"ServerName":             ServerName(team),
			"ServerUnit":             ServerUnit(team),
			"ServerUnitDropIn":       ServerUnitDropIn(team),
			"ServerRuntimeDirectory": ServerRuntimeDirectory(team),
			"FenceUnit":              FenceUnit(team),
			"FenceTable":             FenceTable(team),
			"HomeserverState":        HomeserverState(team),
			"HomeserverPrivateState": HomeserverPrivateState(team),
			"AdminState":             AdminState(team),
			"MigrationDir":           MigrationDir(team),
			"FenceFile":              FenceFile(team),
			"FirewallRecord":         FirewallRecord(team),
			"CtlState":               CtlState(team),
			"TeamLock":               TeamLock(team),
			"StatusTeamDir":          StatusTeamDir(team),
			"StampFile":              StampFile(team),
			"StatusFile":             StatusFile(team),
			"LXCRecord":              LXCRecord(team),
			"ElementProfile":         ElementProfile(team),
			"ElementConfigDir":       dir,
			"ElementLauncher":        launcher,
			"Seat.Bundle":            seat.Bundle(),
			"Seat.Token":             seat.Token(),
			"Seat.Lock":              seat.Lock(),
			"Seat.Store":             seat.Store(),
			"Seat.Report":            seat.Report(),
			"Seat.LastLog":           seat.LastLog(),
		} {
			names[name] = append(names[name], value)
		}
	}
	return names
}

// notNames are the exported identifiers that return a string but are not v2 names: an id's own
// text.
var notNames = []string{"TeamID.String"}

// Every exported constant, and every exported function or method that returns a string, is in
// sampleNames, so the checks below cannot miss a name added later.
func TestEveryExportedNameIsSampled(t *testing.T) {
	sampled := sampleNames(t)
	for _, ident := range exportedStringIdents(t) {
		if _, ok := sampled[ident]; !ok && !slices.Contains(notNames, ident) {
			t.Errorf("%s is exported but not in sampleNames: add it, so the v1 checks cover it", ident)
		}
	}
}

// D 4.1 part 3: no exported name holds a v1 name, no v1 name is a prefix of a v2 name and no v2
// name (or any part of one) is a prefix of a v1 name, which guards against a careless glob or
// rm -r <prefix>*.
func TestNoNameHoldsOrPrefixesAV1Name(t *testing.T) {
	v1 := loadV1Names(t)
	for ident, values := range sampleNames(t) {
		for _, value := range values {
			for _, v := range v1 {
				for _, problem := range v.problems(value) {
					t.Errorf("%s = %q: %s", ident, value, problem)
				}
			}
		}
	}
}

// problems is what is wrong with a v2 value against one v1 name.
func (v v1Name) problems(value string) []string {
	var found []string
	if v.holds(value) {
		found = append(found, "holds the v1 name "+v.name)
	}
	if v.next != "" {
		// The exact form's name is a prefix of v2's Element-teamchat-<team> by Element's own
		// naming; holds() above is its whole rule.
		return found
	}
	for _, part := range append(strings.Split(value, "/"), value) {
		if part != "" && strings.HasPrefix(strings.ToLower(v.name), strings.ToLower(part)) {
			found = append(found, part+" is a prefix of the v1 name "+v.name)
		}
	}
	return found
}

// The prefix check discriminates: a v2 part that begins a v1 name is reported.
func TestPrefixCheckDiscriminates(t *testing.T) {
	for _, v := range loadV1Names(t) {
		if v.next != "" {
			continue
		}
		if got := v.problems(v.name[:2]); len(got) == 0 {
			t.Errorf("a name %q beginning %q is not reported", v.name[:2], v.name)
		}
		if got := v.problems("/var/" + v.name[:2] + "/lib"); !strings.HasPrefix(v.name, "/") && len(got) == 0 {
			t.Errorf("a part %q beginning %q is not reported", v.name[:2], v.name)
		}
	}
}

// notDerived are the exported names D 3.1's derivation rule does not cover: a key inside a
// teamchat_teams entry, and the XDG state home the seat store sits under.
var notDerived = []string{"TeamPortField", "StateHome"}

// D 3.1: every v2 name is formed from the product name.
func TestEveryNameIsDerivedFromTheProduct(t *testing.T) {
	for ident, values := range sampleNames(t) {
		if slices.Contains(notDerived, ident) {
			continue
		}
		for _, value := range values {
			if !strings.Contains(strings.ToLower(value), Product) {
				t.Errorf("%s = %q is not formed from %q", ident, value, Product)
			}
		}
	}
}

// exportedStringIdents lists the package's exported constants, and its exported functions and
// methods whose first result is a string, as Name or Type.Name.
func exportedStringIdents(t *testing.T) []string {
	t.Helper()
	fset := token.NewFileSet()
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatal(err)
	}
	var idents []string
	for _, e := range entries {
		if !strings.HasSuffix(e.Name(), ".go") || strings.HasSuffix(e.Name(), "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			idents = append(idents, declIdents(decl)...)
		}
	}
	if len(idents) == 0 {
		t.Fatal("no exported identifier found: the parse read nothing")
	}
	return idents
}

func declIdents(decl ast.Decl) []string {
	switch d := decl.(type) {
	case *ast.GenDecl:
		return constIdents(d)
	case *ast.FuncDecl:
		return funcIdents(d)
	}
	return nil
}

func constIdents(d *ast.GenDecl) []string {
	if d.Tok != token.CONST {
		return nil
	}
	var idents []string
	for _, spec := range d.Specs {
		value, ok := spec.(*ast.ValueSpec)
		if !ok {
			continue
		}
		for _, n := range value.Names {
			if n.IsExported() {
				idents = append(idents, n.Name)
			}
		}
	}
	return idents
}

func funcIdents(d *ast.FuncDecl) []string {
	if !d.Name.IsExported() || d.Type.Results == nil {
		return nil
	}
	if first, ok := d.Type.Results.List[0].Type.(*ast.Ident); !ok || first.Name != "string" {
		return nil
	}
	if d.Recv == nil {
		return []string{d.Name.Name}
	}
	recv, ok := d.Recv.List[0].Type.(*ast.Ident)
	if !ok {
		return []string{"(unnamed receiver)." + d.Name.Name}
	}
	return []string{recv.Name + "." + d.Name.Name}
}

func sampleRunID(t *testing.T) RunID {
	t.Helper()
	return mustRun(t)
}

func sampleSetID(t *testing.T) SetID {
	t.Helper()
	v, err := ParseSetID("s1")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func sampleGuestUser(t *testing.T) GuestUser {
	t.Helper()
	v, err := ParseGuestUser("dev")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func sampleServerVersion(t *testing.T) ServerVersion {
	t.Helper()
	v, err := ParseServerVersion("1.2.3")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func sampleKitDigest(t *testing.T) Digest {
	t.Helper()
	v, err := ParseDigest("abc123")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func sampleSessionName(t *testing.T) SessionName {
	t.Helper()
	v, err := ParseSessionName("sess-1")
	if err != nil {
		t.Fatal(err)
	}
	return v
}

func sampleCheckoutKey(t *testing.T) CheckoutKey {
	t.Helper()
	v, err := ParseCheckoutKey(strings.Repeat("ab", 16))
	if err != nil {
		t.Fatal(err)
	}
	return v
}
