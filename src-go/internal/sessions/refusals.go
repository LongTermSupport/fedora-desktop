package sessions

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/allowedhosts"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/registry"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/tokens"
)

// The words restore --check gives a record whose launch the next boot's restore would see
// refused before its session starts, from what is on disk or one read-only question to the
// container engine (fedora-desktop#284).
const (
	whySSHKeyMissing       = "ssh-key-missing"
	whyNetworkMissing      = "network-missing"
	whyNetworkCheckFailed  = "network-check-failed"
	whyHostNotAllowed      = "host-not-allowed"
	whyHostCheckFailed     = "host-check-failed"
	whyPassphraseUnusable  = "passphrase-file-unusable"
	whyPoolExpired         = "token-pool-expired"
	unlockDropIn           = ".config/systemd/user/ccy-sessions-restore.service.d/ssh-unlock.conf"
	unlockDropInAssignment = "Environment=" + unlockFileEnv + "="
)

// keptAt ends a refusal's sentence: the record stays where it is.
func keptAt(file string) string {
	return " (the record is kept at " + file + ")."
}

// launchRefusal is why the restore, or the record's launch, would stop before the session
// starts, judged from files alone: the SSH key passphrase file the restore is given (it starts
// nothing when that cannot be used); for ccy, the checkout's allowed-hostnames rule and each
// recorded --ssh-key; for cc, a token pool whose every token has expired.
func (a *App) launchRefusal(rec registry.Record, file string) restorePlan {
	if problem := a.restorePassphraseProblem(); problem != "" {
		return restorePlan{why: whyPassphraseUnusable, sentence: problem + " The restore starts nothing while it cannot be used" + keptAt(file)}
	}
	switch rec.Prefix {
	case prefixCcy:
		if refusal := a.hostRefusal(rec, file); refusal.why != "" {
			return refusal
		}
		return sshKeyRefusal(rec, file)
	case prefixCC:
		return a.tokenPoolRefusal(file)
	}
	return restorePlan{}
}

// prefixCC is a cc session's launcher prefix.
const prefixCC = "cc"

// restorePassphraseProblem is why the passphrase file the restore would be given cannot be
// used, or "": the variable when it is set (as under the restore unit), else the file the
// unit's ssh-unlock drop-in names, %h being HOME. With neither there is no file to check.
func (a *App) restorePassphraseProblem() string {
	file := a.cfg.PassphraseFile
	if file == "" {
		var problem string
		if file, problem = a.dropInPassphraseFile(); problem != "" || file == "" {
			return problem
		}
	}
	return a.passphraseProblem(file)
}

// dropInPassphraseFile is the passphrase file the restore unit's drop-in names, "" when there
// is no drop-in or it names none, and why it could not be read.
func (a *App) dropInPassphraseFile() (string, string) {
	path := filepath.Join(a.Getenv("HOME"), unlockDropIn)
	raw, err := os.ReadFile(filepath.Clean(path))
	if errors.Is(err, fs.ErrNotExist) {
		return "", ""
	}
	if err != nil {
		return "", "the restore unit's drop-in " + path + " could not be read: " + err.Error() + "."
	}
	file := ""
	for line := range strings.Lines(string(raw)) {
		if value, found := strings.CutPrefix(strings.TrimSpace(line), unlockDropInAssignment); found {
			file = value
		}
	}
	if strings.Contains(strings.ReplaceAll(file, "%h", ""), "%") {
		return "", "the restore unit's drop-in " + path + " names its passphrase file with a systemd specifier other than %h, which this check cannot read: " + file + "."
	}
	return strings.ReplaceAll(file, "%h", a.Getenv("HOME")), ""
}

// hostRefusal is the launcher's hostname rule (check_allowed_hostname, allowedhosts) for this
// host in the record's checkout.
func (a *App) hostRefusal(rec registry.Record, file string) restorePlan {
	host, err := a.Hostname()
	var v allowedhosts.Verdict
	if err == nil {
		v, err = allowedhosts.Judge(rec.Dir, host)
	}
	rule := filepath.Join(rec.Dir, allowedhosts.Rel)
	switch {
	case err != nil:
		return restorePlan{why: whyHostCheckFailed, sentence: "whether ccy may run on this host could not be read: " + err.Error() + keptAt(file)}
	case v.Never:
		return restorePlan{why: whyHostNotAllowed, sentence: rule + " says never, so ccy will not run there on any host. Edit that file to restore it" + keptAt(file)}
	case !v.Allowed:
		return restorePlan{why: whyHostNotAllowed, sentence: rule + " allows " + strings.Join(v.Patterns, " ") + ", not this host (" + host +
			"). Add this host to it to restore it" + keptAt(file)}
	}
	return restorePlan{}
}

// recordedValues is every value of flag among the launcher's own arguments of a recorded launch.
func recordedValues(args []string, flag string) []string {
	own := ownArgs(args)
	var values []string
	for i, word := range own {
		if word == flag && i+1 < len(own) {
			values = append(values, own[i+1])
		}
	}
	return values
}

// sshKeyRefusal is the launcher's argument parser's check of each --ssh-key: a regular file,
// a relative one found from the session's folder, where its launch runs.
func sshKeyRefusal(rec registry.Record, file string) restorePlan {
	for _, key := range recordedValues(rec.Args, "--ssh-key") {
		path := key
		if !filepath.IsAbs(path) {
			path = filepath.Join(rec.Dir, path)
		}
		if info, err := os.Stat(path); err != nil || !info.Mode().IsRegular() {
			return restorePlan{why: whySSHKeyMissing, sentence: "its --ssh-key " + key +
				" is not a file, so its launch would stop at once. Declare it with a key that exists" + keptAt(file)}
		}
	}
	return restorePlan{}
}

// tokenPoolRefusal is cc's refusal of a token pool whose every token is past its stamp
// (tokens.Decide in host mode): it will not pick one, nor fall back to another account.
func (a *App) tokenPoolRefusal(file string) restorePlan {
	dir := filepath.Join(a.Getenv("HOME"), ".claude-tokens", "ccy", "tokens")
	pool, err := tokens.ReadPool(dir)
	if err != nil {
		return restorePlan{why: whyTokenCheckFailed, sentence: "cc's token pool " + dir + " could not be read: " + err.Error() + keptAt(file)}
	}
	if d := tokens.Decide(tokens.ModeHost, pool, a.Now()); d.Outcome == tokens.OutcomeRefuse {
		return restorePlan{why: whyPoolExpired, sentence: "every token in " + dir + " is past its expiry stamp (" + strings.Join(d.Expired, ", ") +
			"), and cc will not pick one for you. Mint a fresh one: ccy --create-token" + keptAt(file)}
	}
	return restorePlan{}
}

// networkRefusal is the launcher's check of a recorded --network: the boot launch's engine (the
// record's --engine, else podman) is asked for its networks, read-only, and a launch given one
// it does not list stops ("Network not found"). The caller's CCY_CONTAINER_ENGINE is not read:
// only a login shell sets it, and the restore unit's launch never has it.
func (a *App) networkRefusal(ctx context.Context, rec registry.Record, file string) restorePlan {
	networks := recordedValues(rec.Args, "--network")
	if rec.Prefix != prefixCcy || len(networks) == 0 {
		return restorePlan{}
	}
	network := networks[len(networks)-1]
	var request engine.Request
	if engines := recordedValues(rec.Args, engine.FlagName); len(engines) > 0 {
		request.Flag, request.FlagSet = engines[len(engines)-1], true
	}
	failed := func(kind string, err error) restorePlan {
		return restorePlan{why: whyNetworkCheckFailed, sentence: "the " + kind + " networks could not be listed to check its --network " + network +
			" (" + err.Error() + ")" + keptAt(file)}
	}
	choice, err := engine.Select(request, a.LookPath)
	if err != nil {
		return failed("container engine's", err)
	}
	e, err := engine.New(choice.Kind, a.Runner)
	if err != nil {
		return failed(choice.Kind.String(), err)
	}
	listed, err := e.Networks(ctx)
	if err != nil {
		return failed(choice.Kind.String(), err)
	}
	if !slices.Contains(listed, network) {
		return restorePlan{why: whyNetworkMissing, sentence: "its --network " + network + " is not a " + choice.Kind.String() +
			" network, so its launch would stop at once. Start what makes it, or declare it with another network or --no-network" + keptAt(file)}
	}
	return restorePlan{}
}
