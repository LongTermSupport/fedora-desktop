// Package badrecordfile finds a session record's file by the record's name, every way the rule
// sees.
package badrecordfile

import (
	"path"
	"path/filepath"

	reg "fix/internal/registry"
)

// end is the picker's defect: the session's record removed by its name.
func end(store *reg.Store, name string) error {
	return store.Remove(name) // want `session record's file named from its name`
}

// held is Remove as a method value and a method expression.
func held(store *reg.Store) []func(string) error {
	return []func(string) error{
		store.Remove, // want `session record's file named from its name`
		func(n string) error { return (*reg.Store).Remove(store, n) }, // want `session record's file named from its name`
	}
}

// check is the restore's defect: the directory joined with a name= read from the record.
func check(getenv func(string) string) ([]string, error) {
	regdir, err := reg.Dir(getenv)
	if err != nil {
		return nil, err
	}
	reading, err := reg.New(regdir).ReadAll()
	if err != nil {
		return nil, err
	}
	var out []string
	for _, rec := range reading.Records {
		out = append(out, filepath.Join(regdir, rec.Name)) // want `session record's file named from its name`
	}
	return out, nil
}

// kept is the declare message's defect: the store's directory and the name.
func kept(store *reg.Store, rec reg.Record) string {
	return filepath.Join(store.Dir(), rec.Name) // want `session record's file named from its name`
}

// run holds the directory in a field.
type run struct {
	regdir string
	other  string
}

// byLiteral sets the field in a literal, then joins it.
func byLiteral(store *reg.Store, rec reg.Record) string {
	r := &run{regdir: store.Dir()}
	return path.Join(r.regdir, rec.Name) // want `session record's file named from its name`
}

// byAssignment sets the field by an assignment, then builds the path by +.
func byAssignment(store *reg.Store, rec reg.Record) string {
	var r run
	r.other = store.Dir()
	return r.other + "/" + rec.Name // want `session record's file named from its name`
}

// pkgDir is the directory held in a package variable.
var pkgDir = (&reg.Store{}).Dir()

// fromPackageVar joins the package variable.
func fromPackageVar(name string) string {
	return filepath.Join(pkgDir, "x", name) // want `session record's file named from its name`
}

// relayed passes the directory through a second local.
func relayed(store *reg.Store, name string) string {
	d := store.Dir()
	e := d
	return e + "/" + name // want `session record's file named from its name`
}
