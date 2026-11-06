package host

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/pki"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/state"
)

// The modes of the files in root/<team>/tls/ (SPEC-cross-network X13): the keys are root's alone,
// and the certificates, which every member bundle carries, are readable so that a tool running as
// root and one reading a copy of the directory are not told apart by their bytes.
const (
	keyMode  fs.FileMode = 0o600
	certMode fs.FileMode = 0o644
)

// caAdvice ends every refusal of a CA: the host never replaces one on its own, because every
// bundle issued carries its certificate.
const caAdvice = "a team's CA is never replaced by an apply: restore the file from a team backup, " +
	"or remove both CA files so that the next apply makes a new CA (every bundle issued before it must then be reissued)"

// wallClock is the host's Clock as pki reads it.
type wallClock struct{ Clock }

func (w wallClock) WallMS() int64 { return w.Now().UnixMilli() }

// tls is the step after the first apply's files (X13, X14): the team's CA is made once and never
// replaced, and the leaf its homeserver is reached by is made when there is none, when its names
// are not the wanted names, and when fewer than 30 days of it remain. The leaf's names are every
// derived listen address and the url host when that is a name. A CA that cannot be used is a
// refusal that names the file, and nothing after this step runs.
func (a *applying) tls(_ context.Context) error {
	names, err := a.leafNames()
	if err != nil {
		return a.fail(StepTLS, err)
	}
	if err = mkdirOwned(a.path(paths.TLSDir(a.team)), -1, -1); err != nil {
		return a.fail(StepTLS, err)
	}
	maker := pki.Maker{Clock: wallClock{a.d.Clock}, Rand: a.d.Rand}
	ca, found, err := loadCA(a.d.Root, a.team)
	if err != nil {
		return &Error{Verb: verbApply, Step: StepTLS, Drift: true, Err: err}
	}
	if !found {
		if ca, err = a.makeCA(maker); err != nil {
			return a.fail(StepTLS, err)
		}
	}
	if err = a.ensureLeaf(maker, ca, names); err != nil {
		return a.fail(StepTLS, err)
	}
	return nil
}

// leafNames are what this apply's leaf is reached by.
func (a *applying) leafNames() (pki.Names, error) { return leafNames(a.in.Team.URL, a.listen) }

// makeCA makes the team's CA and writes it without replacing anything. The key goes first: a
// crash between the two files leaves a key that signed nothing and no certificate any bundle
// could carry, which the next apply refuses by name.
func (a *applying) makeCA(m pki.Maker) (pki.CA, error) {
	ca, err := m.NewCA(caName(a.team))
	if err != nil {
		return pki.CA{}, err
	}
	if err = writeCA(a.d.Root, a.team, ca); err != nil {
		return pki.CA{}, err
	}
	a.note("ca made")
	return ca, nil
}

// caName is the name a team's CA is made with, by apply and by a rotation alike.
func caName(team paths.TeamID) string { return "teamchat " + team.String() + " CA" }

// writeCA creates the CA's two files, the key before the certificate, each only if absent.
func writeCA(root string, team paths.TeamID, ca pki.CA) error {
	if err := createOnly(root+paths.CAKeyFile(team), ca.KeyPEM(), keyMode); err != nil {
		return err
	}
	return createOnly(root+paths.CACertFile(team), ca.CertPEM(), certMode)
}

// loadCA reads the team's CA from root. found is false when neither file is there. A lone file,
// a file that is not regular, and a pair pki refuses are errors naming the file.
func loadCA(root string, team paths.TeamID) (pki.CA, bool, error) {
	certFile, keyFile := root+paths.CACertFile(team), root+paths.CAKeyFile(team)
	cert, haveCert, err := state.ReadFile(certFile, fileMax)
	if err != nil {
		return pki.CA{}, false, err
	}
	key, haveKey, err := state.ReadFile(keyFile, fileMax)
	if err != nil {
		return pki.CA{}, false, err
	}
	switch {
	case !haveCert && !haveKey:
		return pki.CA{}, false, nil
	case !haveCert:
		return pki.CA{}, false, fmt.Errorf("the CA certificate %s is not there, but the CA key %s is; %s", certFile, keyFile, caAdvice)
	case !haveKey:
		return pki.CA{}, false, fmt.Errorf("the CA key %s is not there, but the CA certificate %s is; %s", keyFile, certFile, caAdvice)
	}
	if _, err = pki.Fingerprint(cert); err != nil {
		return pki.CA{}, false, fmt.Errorf("%s: %w; %s", certFile, err, caAdvice)
	}
	ca, err := pki.ParseCA(cert, key)
	if err != nil {
		return pki.CA{}, false, fmt.Errorf("%s: %w; %s", keyFile, err, caAdvice)
	}
	return ca, true, nil
}

// ensureLeaf is the leaf step apply shares with the health tick (X31), with the CA this apply
// loaded or made; the line says why the leaf was made.
func (a *applying) ensureLeaf(m pki.Maker, ca pki.CA, want pki.Names) error {
	step := leafStep{root: a.d.Root, team: a.team, maker: m}
	out, err := step.ensure(ca.CertPEM(), func() (pki.CA, error) { return ca, nil }, want)
	if err != nil || !out.made() {
		return err
	}
	a.leafMade = true
	a.note(out.line)
	return nil
}

// CAPEM is the team's CA certificate in PEM, for the admin issuing a member bundle (X15). It is
// read as root from root/<team>/tls/ and judged with the key beside it, so a bundle never carries
// a certificate the host could not sign a leaf with. A team whose CA is not made yet is an error.
func CAPEM(root string, team paths.TeamID) (string, error) {
	ca, found, err := loadCA(root, team)
	if err != nil {
		return "", err
	}
	if !found {
		return "", fmt.Errorf("the CA of team %s is not made yet: team apply makes it", team)
	}
	return string(ca.CertPEM()), nil
}

// BackupFile is one file a team backup carries: its name in the backup, its mode and its bytes.
type BackupFile struct {
	Name string
	Mode fs.FileMode
	Data []byte
}

// CABackupFiles are the files of the team's CA that its backup carries, the certificate and then
// the key, named as they are in root/<team>/tls/ and with the modes they have there (X13). The
// leaf is not among them: the next apply makes it again. A team with no CA yet has none to carry;
// a CA the host could not use is an error, since a backup of it could not be restored.
func CABackupFiles(root string, team paths.TeamID) ([]BackupFile, error) {
	ca, found, err := loadCA(root, team)
	if err != nil || !found {
		return nil, err
	}
	return []BackupFile{
		{Name: filepath.Base(paths.CACertFile(team)), Mode: certMode, Data: ca.CertPEM()},
		{Name: filepath.Base(paths.CAKeyFile(team)), Mode: keyMode, Data: ca.KeyPEM()},
	}, nil
}

// RestoreCA puts the CA files of a backup back under root with their modes. The files are judged
// first: exactly the certificate and the key, each once, with the modes CABackupFiles gives, a pair
// pki accepts. A team that already has the same CA is left as it is; one that has another is
// refused, and nothing is replaced. No file at all is a backup taken before the CA was made.
func RestoreCA(root string, team paths.TeamID, files []BackupFile) error {
	if len(files) == 0 {
		return nil
	}
	ca, err := backupCA(team, files)
	if err != nil {
		return err
	}
	have, found, err := loadCA(root, team)
	if err != nil {
		return err
	}
	if found {
		if !bytes.Equal(have.CertPEM(), ca.CertPEM()) || !bytes.Equal(have.KeyPEM(), ca.KeyPEM()) {
			return fmt.Errorf("the team already has another CA (%s); a restore never replaces it", root+paths.CACertFile(team))
		}
		return nil
	}
	if err = mkdirOwned(root+paths.TLSDir(team), -1, -1); err != nil {
		return err
	}
	return writeCA(root, team, ca)
}

// backupCA is the CA a backup's files hold, when they are exactly the CA's two files.
func backupCA(team paths.TeamID, files []BackupFile) (pki.CA, error) {
	certName, keyName := filepath.Base(paths.CACertFile(team)), filepath.Base(paths.CAKeyFile(team))
	modes := map[string]fs.FileMode{certName: certMode, keyName: keyMode}
	data := map[string][]byte{}
	for _, f := range files {
		mode, known := modes[f.Name]
		_, seen := data[f.Name]
		switch {
		case !known:
			return pki.CA{}, fmt.Errorf("the backup holds %s, which is no file of the CA", f.Name)
		case seen:
			return pki.CA{}, fmt.Errorf("the backup holds %s twice", f.Name)
		case f.Mode != mode:
			return pki.CA{}, fmt.Errorf("the backup's %s has mode %o, not %o", f.Name, f.Mode, mode)
		case len(f.Data) > fileMax:
			return pki.CA{}, fmt.Errorf("the backup's %s is over %d bytes", f.Name, fileMax)
		}
		data[f.Name] = f.Data
	}
	for _, name := range []string{certName, keyName} {
		if _, ok := data[name]; !ok {
			return pki.CA{}, fmt.Errorf("the backup holds no %s", name)
		}
	}
	if _, err := pki.Fingerprint(data[certName]); err != nil {
		return pki.CA{}, fmt.Errorf("the backup's %s: %w", certName, err)
	}
	ca, err := pki.ParseCA(data[certName], data[keyName])
	if err != nil {
		return pki.CA{}, fmt.Errorf("the backup's %s: %w", keyName, err)
	}
	return ca, nil
}
