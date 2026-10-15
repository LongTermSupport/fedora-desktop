package vmtest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

const arch = "x86_64"

var sha256Hex = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (c *cli) cmdFetch(ctx context.Context, args []string) (err error) {
	if len(args) != 1 {
		return usagef("usage: vmtest fetch <base-key>")
	}
	l, err := c.loadLab()
	if err != nil {
		return err
	}
	b, err := c.baseFacts(ctx, l, args[0])
	if err != nil {
		return err
	}
	p, err := c.probeUpstream(ctx, l)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, p.remove()) }()
	_, err = c.fetch(ctx, l, b, p)
	return err
}

// fetch downloads and verifies every artefact the probe lists for the base (E14).
func (c *cli) fetch(ctx context.Context, l *lab, b baseFacts, p *probe) ([]artefact, error) {
	arts, err := p.artefacts(b.Name)
	if err != nil {
		return nil, err
	}
	if len(arts) == 0 {
		return nil, failf("the probe listed no artefact for %s", b.Name)
	}
	for _, a := range arts {
		if err := c.fetchArtefact(ctx, l, a); err != nil {
			return nil, err
		}
	}
	return arts, nil
}

// fetchArtefact downloads one artefact and proves it against the signed CHECKSUM beside it.
// The CHECKSUM's name is structured (variant, compose label, arch), so no listing is scraped.
func (c *cli) fetchArtefact(ctx context.Context, l *lab, a artefact) error {
	dest := l.at(imagesDir, a.File)
	if err := c.download(ctx, a.URL, dest); err != nil {
		return err
	}
	checksumName := "Fedora-" + a.Variant + "-" + a.Label + "-" + arch + "-CHECKSUM"
	dir := a.URL[:strings.LastIndex(a.URL, "/")]
	checksumFile := l.at(imagesDir, checksumName)
	if err := c.download(ctx, dir+"/"+checksumName, checksumFile); err != nil {
		return err
	}
	if err := c.verifySigned(ctx, l, checksumFile); err != nil {
		return err
	}
	expected, err := checksumEntry(checksumFile, a.File)
	if err != nil {
		return failf("%s has no SHA256 entry for %s", checksumName, a.File)
	}
	if expected != a.SHA256 {
		return failf("releases.json (%s) and %s (%s) disagree about %s", a.SHA256, checksumName, expected, a.File)
	}
	c.logf("hashing %s", a.File)
	actual, err := fileSHA256(dest)
	if err != nil {
		return err
	}
	if actual != expected {
		return failf("SHA-256 MISMATCH for %s: expected %s, got %s", a.File, expected, actual)
	}
	c.logf("verified %s against the signed CHECKSUM and releases.json", a.File)
	return nil
}

// checksumEntry is the one `SHA256 (<file>) = <hex>` line's hash.
func checksumEntry(checksumFile, file string) (string, error) {
	body, err := os.ReadFile(checksumFile)
	if err != nil {
		return "", err
	}
	prefix := "SHA256 (" + file + ") = "
	var found []string
	for line := range strings.SplitSeq(string(body), "\n") {
		if hash, ok := strings.CutPrefix(line, prefix); ok && sha256Hex.MatchString(hash) {
			found = append(found, hash)
		}
	}
	if len(found) != 1 {
		return "", errors.New("no single entry")
	}
	return found[0], nil
}

// download fetches url to dest with a conditional GET, so a current copy costs one request.
func (c *cli) download(ctx context.Context, url, dest string) error {
	partial := dest + ".partial"
	if err := removeIfPresent(partial); err != nil {
		return err
	}
	args := []string{"-fL", "--progress-bar", "-o", partial}
	_, statErr := os.Stat(dest)
	if statErr == nil {
		args = append(args, "-z", dest)
	}
	args = append(args, url)
	if _, err := c.env.Runner.Run(ctx, execx.Cmd{Name: "curl", Args: args, Stdout: c.env.Stderr, Stderr: c.env.Stderr}); err != nil {
		return errors.Join(ctxErr(ctx, failf("download failed: %s", url)), removeIfPresent(partial))
	}
	info, err := os.Stat(partial)
	if err == nil && info.Size() > 0 {
		if err := os.Rename(partial, dest); err != nil {
			return failf("cannot move %s into place: %v", partial, err)
		}
		c.logf("downloaded %s (%d bytes)", filepath.Base(dest), info.Size())
		return nil
	}
	if err := removeIfPresent(partial); err != nil {
		return err
	}
	if statErr != nil {
		return failf("no local copy and the server sent nothing: %s", url)
	}
	c.logf("%s is current (not modified upstream)", filepath.Base(dest))
	return nil
}

func removeIfPresent(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return failf("cannot remove %s: %v", path, err)
	}
	return nil
}

// verifySigned verifies a clearsigned CHECKSUM against the branch's Fedora release key, in a
// private keyring removed afterwards. An unverifiable CHECKSUM certifies nothing.
func (c *cli) verifySigned(ctx context.Context, l *lab, file string) (err error) {
	key := filepath.Join(c.env.GPGKeyDir, "RPM-GPG-KEY-fedora-"+l.fedora+"-primary")
	if !readable(key) {
		return failf("no Fedora %s release key at %s", l.fedora, key)
	}
	home, err := os.MkdirTemp(c.env.TempDir, "vmtest-gnupg.*")
	if err != nil {
		return failf("cannot create a keyring directory: %v", err)
	}
	defer func() {
		if rerr := os.RemoveAll(home); rerr != nil {
			err = errors.Join(err, failf("cannot remove %s: %v", home, rerr))
		}
	}()
	res, err := c.env.Runner.Run(ctx, execx.Cmd{Name: "gpg2", Args: []string{"--homedir", home, "--quiet", flagImport, key}})
	if err != nil {
		c.p.Print(c.env.Stderr, string(res.Stderr))
		return ctxErr(ctx, failf("could not import %s", key))
	}
	res, err = c.env.Runner.Run(ctx, execx.Cmd{Name: "gpg2", Args: []string{"--homedir", home, "--verify", file}})
	if err != nil {
		c.p.Print(c.env.Stderr, string(res.Stderr))
		return ctxErr(ctx, failf("GPG signature verification FAILED for %s — refusing the artefact", filepath.Base(file)))
	}
	c.logf("GPG signature verified on %s", filepath.Base(file))
	return nil
}
