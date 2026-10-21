package paths

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"regexp"
	"slices"
	"strings"
	"testing"
)

// The static gate's data (D 4.1), read here so each has one home: the v1 name list, and the
// corpus of lines both this test and the gate's own test must judge alike.
const (
	gateDataDir  = "../../../qa/teamchatseparate"
	v1NamesFile  = "v1-names.txt"
	v1CorpusFile = "v1-names-corpus.tsv"
)

// v1Name is one entry of the list: a name found in any case, or, with next set, a name found
// exactly as written wherever next does not follow it.
type v1Name struct{ name, next string }

// dataLines is a gate data file's lines that are not blank and not a # comment.
func dataLines(t *testing.T, name string) []string {
	t.Helper()
	data, err := fs.ReadFile(os.DirFS(gateDataDir), name)
	if err != nil {
		t.Fatalf("read the gate's %s: %v", name, err)
	}
	var lines []string
	for line := range strings.SplitSeq(string(data), "\n") {
		if strings.TrimSpace(line) != "" && !strings.HasPrefix(line, "#") {
			lines = append(lines, line)
		}
	}
	if len(lines) == 0 {
		t.Fatalf("%s is empty: a check against nothing proves nothing", name)
	}
	return lines
}

func loadV1Names(t *testing.T) []v1Name {
	t.Helper()
	lines := dataLines(t, v1NamesFile)
	names := make([]v1Name, 0, len(lines))
	for _, line := range lines {
		name, next, exact := strings.Cut(strings.TrimSpace(line), " !")
		if !exact {
			next = ""
		}
		names = append(names, v1Name{name: name, next: next})
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

// The matcher here and the gate's are two codes of one rule (the nested qa module cannot be
// imported): both judge the shared corpus, so they cannot drift apart.
func TestV1NameMatcherAgreesWithTheSharedCorpus(t *testing.T) {
	names := loadV1Names(t)
	for _, row := range dataLines(t, v1CorpusFile) {
		want, line, ok := strings.Cut(row, "\t")
		if !ok || want != "hit" && want != "clean" {
			t.Fatalf("corpus row %q: want hit or clean, a tab, and the line", row)
		}
		got := slices.ContainsFunc(names, func(v v1Name) bool { return v.holds(line) })
		if got != (want == "hit") {
			t.Errorf("%q: held a v1 name = %v, the corpus says %s", line, got, want)
		}
	}
}

// sampleTeams are the team ids every team-derived name is sampled for: the shortest, a typical
// and the longest.
var sampleTeams = []string{"x", "ops", strings.Repeat("z", 22)}

// sampleNames is every exported name of the package, expanded: a constant by its value, a
// function by its value for sample identifiers. The completeness test below holds this table
// to the package's exported identifiers.
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
		"LibDir": LibDir, "ShareDir": ShareDir, "ResolverStub": ResolverStub, "EtcDir": EtcDir,
		"OwnerGroupFile": OwnerGroupFile, "ServerDir": ServerDir, "ServerPinFile": ServerPinFile,
		"HostKitRoot": HostKitRoot, "HostKitCurrent": string(HostKitCurrent),
		"HomeserverStateRoot": HomeserverStateRoot, "HomeserverPrivateStateRoot": HomeserverPrivateStateRoot,
		"AdminStateRoot": AdminStateRoot, "AdminLXCDir": AdminLXCDir, "CtlStateRoot": CtlStateRoot,
		"StatusRoot": StatusRoot, "StatusInstalledFile": StatusInstalledFile,
		"AdminRunDir": AdminRunDir, "ServerUnitTemplate": ServerUnitTemplate,
		"FenceUnitTemplate": FenceUnitTemplate, "ServerStateDirectory": ServerStateDirectory,
		"HealthService": HealthService, "HealthTimer": HealthTimer, "BackupService": BackupService,
		"BackupTimer": BackupTimer, "SeatsService": SeatsService, "SeatsTimer": SeatsTimer,
		"SyslogServer": SyslogServer, "SyslogHealth": SyslogHealth, "SyslogBackup": SyslogBackup,
		"HostVarTeams": HostVarTeams, "HostVarAddress": HostVarAddress,
		"HostVarLXCMembers": HostVarLXCMembers, "HostVarElementTeams": HostVarElementTeams,
		"HostVarMigrations": HostVarMigrations, "OnlyTeamsVar": OnlyTeamsVar, "PlayFile": PlayFile,
		"ElementPlayFile": ElementPlayFile, "AnsibleTag": AnsibleTag, "TeamPortField": TeamPortField,
		"CCYKitDir": string(CCYKitDir), "SessionSeatMount": SessionSeatMount,
		"SessionMembershipMount": SessionMembershipMount, "CCYRunRoot": CCYRunRoot,
	}
	names := map[string][]string{}
	for ident, value := range values {
		names[ident] = []string{value}
	}
	return names
}

// idNames is every function of an identifier other than a team id, for a sample of each, and the
// kit's files in each kit kind.
func idNames(t *testing.T) map[string][]string {
	t.Helper()
	set, user, version, digest := sampleSetID(t), sampleGuestUser(t), sampleServerVersion(t), sampleDigest(t)
	names := map[string][]string{
		"SessionUnit":         {SessionUnit(user)},
		"GuestRuntimeDir":     {GuestRuntimeDir(user)},
		"ServerBinary":        {ServerBinary(version)},
		"AdminBinaryCopy":     {AdminBinaryCopy(digest)},
		"HostKitDir":          {string(HostKitDir(digest))},
		"SetLock":             {SetLock(set)},
		"SetRecord":           {SetRecord(set)},
		"ElementLauncherName": {ElementLauncherName(mustDisplay(t, "Ops"))},
	}
	for _, kit := range []Kit{HostKitCurrent, HostKitDir(digest), CCYKitDir} {
		for ident, value := range map[string]string{
			"Kit.Binary": kit.Binary(), "Kit.PluginDir": kit.PluginDir(),
			"Kit.PluginHooks": kit.PluginHooks(), "Kit.Settings": kit.Settings(),
		} {
			names[ident] = append(names[ident], value)
		}
	}
	return names
}

// baseNames is every function of a base directory, and the owner's seat store under one.
func baseNames(t *testing.T) map[string][]string {
	t.Helper()
	session, key := sampleSessionName(t), sampleCheckoutKey(t)
	stateHome, err := StateHome("/home/u", "")
	if err != nil {
		t.Fatal(err)
	}
	store, err := SeatStore(stateHome)
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
		"StateHome":                {stateHome},
		"SeatStore":                {store.Root},
		"AllocLock":                {alloc},
		"ElementConsentFile":       {consent},
		"Store.Pool":               {store.Pool()},
		"Store.PoolCheckout":       {store.PoolCheckout(key)},
		"Store.PoolCheckoutRecord": {store.PoolCheckoutRecord(key)},
		"Store.Session":            {store.Session(session)},
		"Store.Control":            {store.Control(session)},
		"Store.DesiredFile":        {store.DesiredFile(session)},
		"Store.Released":           {store.Released()},
	}
}

// layoutNames is both member layouts by their fields, and a session runtime under each.
func layoutNames(t *testing.T) map[string][]string {
	t.Helper()
	home, err := HomeLayout("/home/u", "/run/user/1000")
	if err != nil {
		t.Fatal(err)
	}
	names := map[string][]string{}
	for ident, l := range map[string]Layout{"CCYLayout": CCYLayout(), "HomeLayout": home} {
		r := l.Run(sampleRunID(t))
		for name, value := range map[string][]string{
			ident:                     {l.KitRoot, string(l.CurrentKit), l.State, l.Desired, l.RunRoot},
			"Layout.DesiredFile":      {l.DesiredFile()},
			"Layout.ConversationFile": {l.ConversationFile()},
			"Layout.Run":              {r.Dir},
			"Run.KeeperLock":          {r.KeeperLock()},
			"Run.KeeperJSON":          {r.KeeperJSON()},
			"Run.KeeperLog":           {r.KeeperLog()},
			"Run.LinkSock":            {r.LinkSock()},
			"Run.LinkLock":            {r.LinkLock()},
			"Run.LinkLog":             {r.LinkLog()},
			"Run.Settings":            {r.Settings()},
			"Run.Session":             {r.Session()},
			"Run.PID":                 {r.PID()},
			"Run.Status":              {r.Status()},
		} {
			names[name] = append(names[name], value...)
		}
	}
	return names
}

// teamNames is every function of a team id, for each sample team.
func teamNames(t *testing.T) map[string][]string {
	t.Helper()
	names := map[string][]string{}
	for _, s := range sampleTeams {
		team := mustTeam(t, s)
		dir, err := ElementConfigDir("/home/u", team)
		if err != nil {
			t.Fatal(err)
		}
		launcher, err := ElementLauncher("/home/u", team)
		if err != nil {
			t.Fatal(err)
		}
		probe, err := ProbeState("/home/u/.local/state", team)
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
			"ServerConfig":           ServerConfig(team),
			"TeamDeclaration":        TeamDeclaration(team),
			"RestoreBlock":           RestoreBlock(team),
			"MigrationDir":           MigrationDir(team),
			"FenceFile":              FenceFile(team),
			"FirewallRecord":         FirewallRecord(team),
			"CtlState":               CtlState(team),
			"TeamLock":               TeamLock(team),
			"StatusTeamDir":          StatusTeamDir(team),
			"StampFile":              StampFile(team),
			"StatusFile":             StatusFile(team),
			"ApprovedFile":           ApprovedFile(team),
			"LXCOnlineFile":          LXCOnlineFile(team),
			"LXCRecord":              LXCRecord(team),
			"ElementProfile":         ElementProfile(team),
			"ElementConfigDir":       dir,
			"ElementLauncher":        launcher,
			"ProbeState":             probe,
			"Layout.Seat":            seat.Dir,
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

// notName is whether an exported identifier is not a v2 name: a parser, an id's own text, or
// the error the parsers wrap.
func notName(ident string) bool {
	return strings.HasPrefix(ident, "Parse") || ident == "TeamID.String" || ident == "ErrInvalid"
}

// Every exported constant, variable, function and method is either in sampleNames or not a
// name, so the checks below cannot miss a name added later, whatever its type.
func TestEveryExportedNameIsSampled(t *testing.T) {
	sampled := sampleNames(t)
	idents := exportedIdents(t)
	for _, ident := range idents {
		if _, ok := sampled[ident]; !ok && !notName(ident) {
			t.Errorf("%s is exported but not in sampleNames: add it, so the v1 checks cover it", ident)
		}
	}
	for ident := range sampled {
		if !slices.Contains(idents, ident) {
			t.Errorf("sampleNames has %s, which the package does not export", ident)
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

// A team id would make a team-derived name equal a fixed one when it is what follows teamchat-
// in a fixed name (team ctl's homeserver user teamchat-ctl is CtlUser). Each such id, cut where
// the team grammar ends, is reserved, and reservedTeamIDs holds exactly these.
func TestReservedTeamIDsAreExactlyTheFixedNamesTails(t *testing.T) {
	var tails []string
	for _, values := range constNames() {
		for part := range strings.SplitSeq(values[0], "/") {
			tail, ok := strings.CutPrefix(part, Product+"-")
			if !ok {
				continue
			}
			if end := strings.IndexFunc(tail, func(r rune) bool {
				return (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-'
			}); end >= 0 {
				tail = tail[:end]
			}
			if !slices.Contains(tails, tail) {
				tails = append(tails, tail)
			}
		}
	}
	slices.Sort(tails)
	if !slices.Equal(tails, reservedTeamIDs) {
		t.Fatalf("the fixed names' tails are %q; reservedTeamIDs is %q", tails, reservedTeamIDs)
	}
	for _, tail := range tails {
		if _, err := ParseTeamID(tail); err == nil {
			t.Errorf("team id %q is accepted, yet a fixed name is teamchat-%s", tail, tail)
		}
	}
}

// perTeamRoots are the directories whose children are named by team id.
var perTeamRoots = []string{
	HomeserverStateRoot, HomeserverPrivateStateRoot, AdminStateRoot, CtlStateRoot, StatusRoot + "/teams",
}

// A fixed child of a per-team root can never be a team's directory: every first segment under
// one that is not a sample team is refused as a team id (C's LXC records are under _lxc, the
// migration sets' locks under _sets).
func TestNoFixedChildOfAPerTeamRootIsATeamID(t *testing.T) {
	for ident, values := range sampleNames(t) {
		for _, value := range values {
			if segment := teamLikeChild(value); segment != "" {
				t.Errorf("%s = %q: %s under a per-team root is a valid team id", ident, value, segment)
			}
		}
	}
}

var firstSegment = regexp.MustCompile(`^[^/]*`)

// teamLikeChild is the first segment of value under a per-team root when it is a valid team id
// other than a sample team, else "".
func teamLikeChild(value string) string {
	for _, root := range perTeamRoots {
		rest, ok := strings.CutPrefix(value, root+"/")
		if !ok {
			continue
		}
		segment := firstSegment.FindString(rest)
		if _, err := ParseTeamID(segment); err == nil && !slices.Contains(sampleTeams, segment) {
			return segment
		}
	}
	return ""
}

// The check above can fail: C's LXC records where C first put them, under lxc/, are reported,
// and the same records under _lxc/ are not.
func TestThePerTeamRootCheckDiscriminates(t *testing.T) {
	if got := teamLikeChild(AdminStateRoot + "/lxc/c1/dev.json"); got != "lxc" {
		t.Errorf("lxc/ under %s: got %q, want lxc", AdminStateRoot, got)
	}
	for _, value := range []string{AdminLXCDir + "/c1/dev.json", AdminStateRoot + "/ops/team.json", StatusRoot + "/lxc/ops.json"} {
		if got := teamLikeChild(value); got != "" {
			t.Errorf("%s: got %q, want nothing", value, got)
		}
	}
}

// exportedIdents lists the package's exported constants, variables, functions and methods, as
// Name or Type.Name.
func exportedIdents(t *testing.T) []string {
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
		return valueIdents(d)
	case *ast.FuncDecl:
		return funcIdents(d)
	}
	return nil
}

func valueIdents(d *ast.GenDecl) []string {
	if d.Tok != token.CONST && d.Tok != token.VAR {
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
	if !d.Name.IsExported() {
		return nil
	}
	if d.Recv == nil {
		return []string{d.Name.Name}
	}
	recv := d.Recv.List[0].Type
	if star, ok := recv.(*ast.StarExpr); ok {
		recv = star.X
	}
	if ident, ok := recv.(*ast.Ident); ok {
		return []string{ident.Name + "." + d.Name.Name}
	}
	return []string{"(unnamed receiver)." + d.Name.Name}
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

func sampleDigest(t *testing.T) Digest {
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
