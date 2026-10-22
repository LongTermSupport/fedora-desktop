package paths

import (
	"encoding/json"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// guarded are the types only this package makes valid; a name derived from the zero value of
// any of them panics, since the name would be the parent directory of every team or session.
var guarded = []string{
	"TeamID", "RunID", "CheckoutKey", "GuestUser", "SetID", "SessionName", "Digest", "DisplayName",
	"ServerVersion", "Kit", "Layout", "Seat", "Run", "Store",
}

// zeroDerivations is every exported function that takes a guarded type, given the zero value
// of it (and a valid base directory where it takes one).
func zeroDerivations() map[string]func() string {
	orError := func(s string, err error) string {
		if err != nil {
			return err.Error()
		}
		return s
	}
	var (
		team    TeamID
		user    GuestUser
		version ServerVersion
		digest  Digest
		set     SetID
		display DisplayName
		key     CheckoutKey
	)
	return map[string]func() string{
		"HomeserverUser":         func() string { return HomeserverUser(team) },
		"ServerName":             func() string { return ServerName(team) },
		"ServerUnit":             func() string { return ServerUnit(team) },
		"ServerUnitDropIn":       func() string { return ServerUnitDropIn(team) },
		"ServerRuntimeDirectory": func() string { return ServerRuntimeDirectory(team) },
		"FenceUnit":              func() string { return FenceUnit(team) },
		"FenceTable":             func() string { return FenceTable(team) },
		"SessionUnit":            func() string { return SessionUnit(user) },
		"GuestRuntimeDir":        func() string { return GuestRuntimeDir(user) },
		"ServerBinary":           func() string { return ServerBinary(version) },
		"AdminBinaryCopy":        func() string { return AdminBinaryCopy(digest) },
		"HostKitDir":             func() string { return HostKitDir(digest).String() },
		"HomeserverState":        func() string { return HomeserverState(team) },
		"HomeserverPrivateState": func() string { return HomeserverPrivateState(team) },
		"AdminState":             func() string { return AdminState(team) },
		"ServerConfig":           func() string { return ServerConfig(team) },
		"TeamDeclaration":        func() string { return TeamDeclaration(team) },
		"RestoreBlock":           func() string { return RestoreBlock(team) },
		"MigrationDir":           func() string { return MigrationDir(team) },
		"FenceFile":              func() string { return FenceFile(team) },
		"FirewallRecord":         func() string { return FirewallRecord(team) },
		"CtlState":               func() string { return CtlState(team) },
		"RosterFile":             func() string { return RosterFile(team) },
		"RoomsFile":              func() string { return RoomsFile(team) },
		"TeamLock":               func() string { return TeamLock(team) },
		"SetLock":                func() string { return SetLock(set) },
		"StatusTeamDir":          func() string { return StatusTeamDir(team) },
		"StampFile":              func() string { return StampFile(team) },
		"StatusFile":             func() string { return StatusFile(team) },
		"ApprovedFile":           func() string { return ApprovedFile(team) },
		"LXCOnlineFile":          func() string { return LXCOnlineFile(team) },
		"LXCRecord":              func() string { return LXCRecord(team) },
		"SetRecord":              func() string { return SetRecord(set) },
		"ElementProfile":         func() string { return ElementProfile(team) },
		"ElementLauncherName":    func() string { return ElementLauncherName(display) },
		"ElementConfigDir":       func() string { return orError(ElementConfigDir("/home/u", team)) },
		"ElementLauncher":        func() string { return orError(ElementLauncher("/home/u", team)) },
		"ProbeState":             func() string { return orError(ProbeState("/home/u/.local/state", team)) },
		"AllocLock":              func() string { return orError(AllocLock("/run/user/1000", key)) },
	}
}

// panics is whether f panics; got is what it returned otherwise.
func panics(f func() string) (bool, string) {
	did := true
	var got string
	func() {
		defer func() {
			if recover() == nil {
				did = false
			}
		}()
		got = f()
	}()
	return did, got
}

func TestEveryFunctionOfAZeroValuePanics(t *testing.T) {
	derivations := zeroDerivations()
	for name, f := range derivations {
		if did, got := panics(f); !did {
			t.Errorf("%s of a zero value returned %q", name, got)
		}
	}
	takers := guardedTakers(t)
	for _, name := range takers {
		if _, ok := derivations[name]; !ok {
			t.Errorf("%s takes a guarded type but is not in zeroDerivations: add it", name)
		}
	}
	for name := range derivations {
		if !slices.Contains(takers, name) {
			t.Errorf("zeroDerivations has %s, which takes no guarded type", name)
		}
	}
}

// guardedTakers lists the package's exported functions (not methods) with a parameter of a
// guarded type.
func guardedTakers(t *testing.T) []string {
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
		file, err := parser.ParseFile(fset, e.Name(), nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && fn.Name.IsExported() && takesGuarded(fn) {
				names = append(names, fn.Name.Name)
			}
		}
	}
	if len(names) == 0 {
		t.Fatal("no function takes a guarded type: the parse read nothing")
	}
	return names
}

func takesGuarded(fn *ast.FuncDecl) bool {
	for _, field := range fn.Type.Params.List {
		if ident, ok := field.Type.(*ast.Ident); ok && slices.Contains(guarded, ident.Name) {
			return true
		}
	}
	return false
}

// Every method of a zero kit, layout, seat, run or store panics but String, and every method
// of a valid one panics when given a zero id.
func TestEveryMethodOfAZeroValuePanics(t *testing.T) {
	store, err := SeatStore("/home/u/.local/state")
	if err != nil {
		t.Fatal(err)
	}
	valid := map[reflect.Type]reflect.Value{
		reflect.TypeFor[TeamID]():      reflect.ValueOf(mustTeam(t, "ops")),
		reflect.TypeFor[RunID]():       reflect.ValueOf(mustRun(t)),
		reflect.TypeFor[SessionName](): reflect.ValueOf(sampleSessionName(t)),
		reflect.TypeFor[CheckoutKey](): reflect.ValueOf(sampleCheckoutKey(t)),
	}
	for _, recv := range []struct{ zero, valid any }{
		{Kit{}, HostKitCurrent()},
		{Layout{}, CCYLayout()},
		{Seat{}, CCYLayout().Seat(mustTeam(t, "ops"))},
		{Run{}, CCYLayout().Run(mustRun(t))},
		{Store{}, store},
	} {
		zero, good := reflect.ValueOf(recv.zero), reflect.ValueOf(recv.valid)
		for i := range zero.NumMethod() {
			m := zero.Type().Method(i)
			if m.Name == "String" {
				continue
			}
			args, zeroArgs := methodArgs(t, m.Type, valid)
			if !callPanics(zero.Method(i), args) {
				t.Errorf("%s.%s of the zero value does not panic", zero.Type().Name(), m.Name)
			}
			if callPanics(good.Method(i), args) {
				t.Errorf("%s.%s of a valid value panics", zero.Type().Name(), m.Name)
			}
			if len(zeroArgs) > 0 && !callPanics(good.Method(i), zeroArgs) {
				t.Errorf("%s.%s of a zero id does not panic", zero.Type().Name(), m.Name)
			}
		}
	}
}

// methodArgs is a valid argument list for a method, and the same with zero ids (nil when it
// takes none).
func methodArgs(t *testing.T, method reflect.Type, valid map[reflect.Type]reflect.Value) ([]reflect.Value, []reflect.Value) {
	t.Helper()
	var good, zero []reflect.Value
	for i := 1; i < method.NumIn(); i++ {
		in := method.In(i)
		v, ok := valid[in]
		if !ok {
			t.Fatalf("no valid %s to call a method with: add one", in)
		}
		good = append(good, v)
		zero = append(zero, reflect.Zero(in))
	}
	return good, zero
}

func callPanics(m reflect.Value, args []reflect.Value) bool {
	did, _ := panics(func() string {
		m.Call(args)
		return ""
	})
	return did
}

// A decode leaves a missing or null id field zero without calling UnmarshalText; the first name
// derived from it panics rather than naming every team's directory.
func TestADecodedMissingIDPanicsWhenUsed(t *testing.T) {
	for _, doc := range []string{`{}`, `{"team":null}`} {
		var got struct {
			Team TeamID `json:"team"`
		}
		if err := json.Unmarshal([]byte(doc), &got); err != nil {
			t.Fatalf("%s: %v", doc, err)
		}
		if did, name := panics(func() string { return AdminState(got.Team) }); !did {
			t.Errorf("%s: AdminState of the decoded id is %q", doc, name)
		}
	}
}

// A port decodes from a JSON number only through ParsePort, and its zero value is neither encoded
// nor read.
func TestPortDecodesOnlyThroughParsePort(t *testing.T) {
	var p Port
	if err := json.Unmarshal([]byte(`8448`), &p); err != nil || p.Int() != 8448 {
		t.Fatalf("8448: %v", err)
	}
	if data, err := json.Marshal(p); err != nil || string(data) != "8448" {
		t.Fatalf("round trip: %s, %v", data, err)
	}
	for _, doc := range []string{`0`, `65536`, `-1`, `null`, `"8448"`, `84.5`} {
		var q Port
		if err := json.Unmarshal([]byte(doc), &q); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s decoded: %v", doc, err)
		}
	}
	if _, err := json.Marshal(Port{}); !errors.Is(err, ErrInvalid) {
		t.Errorf("Port{} encoded: %v", err)
	}
	if did, got := panics(func() string { return strconv.Itoa(Port{}.Int()) }); !did {
		t.Errorf("Port{}.Int() returned %q", got)
	}
}
