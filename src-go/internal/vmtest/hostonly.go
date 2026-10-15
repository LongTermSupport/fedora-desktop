package vmtest

import (
	"bytes"
	"context"
	"os"
	"regexp"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
)

// A host-only scenario puts a REAL GitHub PAT and SSH passphrase into a guest (Plan 00121).
// This CLI is one of three independent gates on it: the scenario is absent from the bridge
// allowlist so the watcher rejects it, and bridge_run refuses it from the manifest before
// dispatch. Any one alone would be a single point of failure.

// guestDropDir is tmpfs inside the guest, so nothing here touches the overlay that outlives
// the run.
const guestDropDir = "/dev/shm/vmtest-secrets"

var githubUserRE = regexp.MustCompile(`^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$`)

// credentials are the host-only options: the secrets named by path, never by value.
type credentials struct {
	account, tokenFile, ppFile string
}

func (cr credentials) any() bool { return cr.account+cr.tokenFile+cr.ppFile != "" }

// listed reports whether id is a whole line of the file, compared literally (B6). A file that
// cannot be read lists nothing: a missing list is never permission.
func listed(path, id string) bool {
	body, err := os.ReadFile(path)
	if err != nil {
		return false
	}
	for line := range strings.SplitSeq(string(body), "\n") {
		if line == id {
			return true
		}
	}
	return false
}

// hostOnlyPreflight is every gate a credential-bearing run passes before anything boots, in
// order (E30). No refusal names a secret's value.
func (c *cli) hostOnlyPreflight(l *lab, id string, cr credentials) error {
	if c.env.Getenv("VMTEST_FROM_BRIDGE") == "1" {
		return failf("%s handles a real credential and was launched by the bridge; it is run by a human at the host CLI only", id)
	}
	if !listed(l.at(hostOnlyFile), id) {
		return failf("%s is not in the deployed host-only list %s; re-run %s on the host", id, l.at(hostOnlyFile), playName)
	}
	// Disjoint by construction in scenarios.py: on disk, both means a deployed file was edited
	// by hand, and the one edited is the one keeping this run's PAT away from the bridge.
	if listed(l.at(allowlistFile), id) {
		return failf("%s is on both the bridge allowlist and the host-only list; they are built disjoint, so one of them has been edited by hand", id)
	}
	switch {
	case cr.account == "":
		return failf("%s needs --github-account: the throwaway GitHub account this run authenticates as", id)
	case !githubUserRE.MatchString(cr.account):
		return failf("--github-account '%s' is not a GitHub username (alphanumerics and single hyphens)", cr.account)
	case cr.tokenFile == "":
		return failf("%s needs --github-token-file: a 0600 file holding a PAT for a THROWAWAY account, carrying every scope in vars/github-required-scopes.yml", id)
	case cr.ppFile == "":
		return failf("%s needs --github-ssh-passphrase-file: a 0600 file holding the passphrase run.bash will protect the generated login key with", id)
	}
	for _, secret := range []string{cr.tokenFile, cr.ppFile} {
		if err := secretFileOK(secret); err != nil {
			return err
		}
	}
	return nil
}

// secretFileOK is a readable, non-empty regular file only its owner can read (E30, B7).
func secretFileOK(path string) error {
	info, err := os.Stat(path)
	if err != nil || !readable(path) {
		return failf("secret file %s is not readable", path)
	}
	if !info.Mode().IsRegular() {
		return failf("secret file %s is not a regular file", path)
	}
	if info.Size() == 0 {
		return failf("secret file %s is empty; an empty secret cannot be redacted from the transcript afterwards, nor verified absent from it", path)
	}
	if perm := info.Mode().Perm(); perm != 0o600 && perm != 0o400 {
		return failf("secret file %s is mode %o; it must be 0600 or 0400, or every account on this host can read it before the run even starts", path, perm)
	}
	return nil
}

// pushSecrets delivers the two secrets into guest tmpfs by scp, which never puts the content
// on a command line (E44).
func (g *guest) pushSecrets(ctx context.Context, cr credentials) error {
	if err := g.do(ctx, "umask 077 && mkdir -p "+guestDropDir+" && chmod 0700 "+guestDropDir); err != nil {
		return err
	}
	if err := g.scp(ctx, cr.tokenFile, guestDropDir+"/gh-token"); err != nil {
		return err
	}
	if err := g.scp(ctx, cr.ppFile, guestDropDir+"/ssh-passphrase"); err != nil {
		return err
	}
	return g.do(ctx, "chmod 0600 "+guestDropDir+"/gh-token "+guestDropDir+"/ssh-passphrase")
}

// dropSecrets removes what the harness delivered before the checker looks: run.bash unlinking
// its own transient files is what the scenario tests, and these two are the harness's.
func (g *guest) dropSecrets(ctx context.Context) error {
	return g.do(ctx, "rm -f "+guestDropDir+"/gh-token "+guestDropDir+"/ssh-passphrase")
}

// pushNeedles writes the exact bytes the checker searches for, one per line, on stdin. The
// trailing newlines belong to the host's files, not to the secrets (E44).
func (g *guest) pushNeedles(ctx context.Context, cr credentials) error {
	if err := g.do(ctx, "umask 077 && mkdir -p "+guestDropDir+" && chmod 0700 "+guestDropDir); err != nil {
		return err
	}
	var needles bytes.Buffer
	for _, f := range []string{cr.tokenFile, cr.ppFile} {
		body, err := os.ReadFile(f)
		if err != nil {
			return failf("cannot read secret file %s", f)
		}
		needles.WriteString(strings.TrimRight(string(body), "\n") + "\n")
	}
	return g.doWith(ctx, "umask 077 && cat > "+guestDropDir+"/needles", &needles)
}

// scrub redacts the supplied secrets from the run's artefacts and refuses if any survive
// (E45). The verify half is the point: a scrubber is fail-open by nature.
func (c *cli) scrub(ctx context.Context, l *lab, cr credentials, runDir, transcript, console string) error {
	front := l.at(scrubFront)
	for _, f := range []string{front, l.at(scrubLibrary)} {
		if !readable(f) {
			return failf("no %s: this run handled a real credential and its artefacts cannot be checked for residue; re-run %s on the host", f, playName)
		}
	}
	for _, artefact := range []string{transcript, console} {
		if _, err := os.Stat(artefact); err != nil {
			if artefact == transcript {
				return failf("the transcript %s is missing; the run's artefacts cannot be checked for residue", transcript)
			}
			c.logf("no %s to scrub", artefact)
			continue
		}
		for _, argv := range [][]string{
			{front, "redact", artefact, cr.tokenFile, "github-token"},
			{front, "redact", artefact, cr.ppFile, "ssh-passphrase"},
			{front, "verify", artefact, cr.tokenFile, cr.ppFile},
		} {
			if _, err := c.env.Runner.Run(ctx, execx.Cmd{Name: "bash", Args: argv, Stdout: c.env.Stderr, Stderr: c.env.Stderr}); err != nil {
				return ctxErr(ctx, failf("a supplied secret survived redaction in %s; the run's artefacts are NOT safe to share (they remain under %s)", artefact, runDir))
			}
		}
	}
	return nil
}
