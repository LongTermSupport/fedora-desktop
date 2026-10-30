package containerimages

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/engine"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// defaultOld is the old range triage assumes unless told: the first range Fedora hands out.
var defaultOld = Range{Start: 100000, Count: 65536}

const oldRangeUsage = `
  old-range scan --user NAME
                 (root) walk NAME's rootless store once and print, as JSON, each store
                 directory holding files owned outside root, NAME and NAME's subuid range
  old-range triage --scan FILE [--old-start N --old-count N] [--proposed FILE]
                 (the user) each such directory attributed to an image, container, volume
                 or nothing, with a proposed action; a markdown report on stdout, the
                 proposed decisions written to --proposed
  old-range plan --scan FILE --decisions FILE
                 (the user) the decisions checked against the store now; the plan as JSON
  old-range reown --plan FILE [--yes]
                 (root) re-own the plan's directories into the current range, remove the
                 strays it names; a dry run without --yes
  old-range remove --plan FILE [--yes]
                 (the user) podman's own removals of what the plan removes, logged; a dry
                 run without --yes
`

// oldRangeFlags is each subcommand's flags, and those it needs.
var oldRangeFlags = map[string]struct{ allowed, required []string }{
	"scan":   {[]string{flagUser}, []string{flagUser}},
	"triage": {[]string{flagScan, flagOldStart, flagOldCount, flagProposed}, []string{flagScan}},
	"plan":   {[]string{flagScan, flagDecisions}, []string{flagScan, flagDecisions}},
	"reown":  {[]string{flagPlan, flagYes}, []string{flagPlan}},
	"remove": {[]string{flagPlan, flagYes}, []string{flagPlan}},
}

const (
	flagYes       = "--yes"
	flagUser      = "--user"
	flagScan      = "--scan"
	flagPlan      = "--plan"
	flagDecisions = "--decisions"
	flagProposed  = "--proposed"
	flagOldStart  = "--old-start"
	flagOldCount  = "--old-count"
)

// oldRangeArgs is one old-range command line.
type oldRangeArgs struct {
	sub    string
	values map[string]string
	old    Range
}

func (o oldRangeArgs) yes() bool { _, ok := o.values[flagYes]; return ok }

func parseOldRange(args []string) (oldRangeArgs, error) {
	if len(args) == 0 {
		return oldRangeArgs{}, errors.New("old-range needs a subcommand")
	}
	spec, ok := oldRangeFlags[args[0]]
	if !ok {
		return oldRangeArgs{}, fmt.Errorf("old-range has no subcommand %q", args[0])
	}
	o := oldRangeArgs{sub: args[0], values: map[string]string{}, old: defaultOld}
	for i := 1; i < len(args); i++ {
		flag := args[i]
		if _, dup := o.values[flag]; dup || !slices.Contains(spec.allowed, flag) {
			return o, fmt.Errorf("old-range %s does not take %q here", o.sub, flag)
		}
		if flag == "--yes" {
			o.values[flag] = ""
			continue
		}
		if i+1 >= len(args) || args[i+1] == "" {
			return o, fmt.Errorf("old-range %s: %s needs a value", o.sub, flag)
		}
		o.values[flag] = args[i+1]
		i++
	}
	for _, flag := range spec.required {
		if _, ok := o.values[flag]; !ok {
			return o, fmt.Errorf("old-range %s needs %s", o.sub, flag)
		}
	}
	for flag, into := range map[string]*int64{"--old-start": &o.old.Start, "--old-count": &o.old.Count} {
		text, given := o.values[flag]
		if !given {
			continue
		}
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil || n <= 0 {
			return o, fmt.Errorf("old-range %s: %s is a positive number, not %q", o.sub, flag, text)
		}
		*into = n
	}
	return o, nil
}

// oldRange is `old-range SUBCOMMAND ...` (O1-O15).
func (a App) oldRange(ctx context.Context, args []string) term.ExitCode {
	o, err := parseOldRange(args)
	if err != nil {
		if code := a.print(a.Env.Stderr, "container-images: "+err.Error()+"\n"+usage); code != term.ExitOK {
			return code
		}
		return term.ExitUsage
	}
	switch o.sub {
	case "scan":
		err = a.scan(o.values["--user"])
	case "triage":
		err = a.triageCmd(ctx, o.values["--scan"], o.old, o.values["--proposed"])
	case "plan":
		err = a.planCmd(ctx, o.values["--scan"], o.values["--decisions"])
	case "reown":
		err = a.reown(o.values["--plan"], o.yes())
	default:
		return a.removeOld(ctx, o.values["--plan"], o.yes())
	}
	if err != nil {
		return a.fail(err)
	}
	return term.ExitOK
}

// notRoot is O14: the user's half never runs as root, since podman's view is the user's.
func (a App) notRoot(sub string) error {
	if a.Env.EUID == 0 {
		return fmt.Errorf("old-range %s runs as the store's user, never as root: podman's view of the store is the user's", sub)
	}
	return nil
}

// asUser is O14: the user's half runs as the user the scan or plan is of.
func (a App) asUser(uid int64) error {
	if int64(a.Env.EUID) != uid {
		return fmt.Errorf("this store is uid %d's, and this runs as uid %d: run it as the store's user", uid, a.Env.EUID)
	}
	return nil
}

func readScan(path string) (Scan, error) {
	var s Scan
	text, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return s, fmt.Errorf("cannot read the scan %s: %w", path, err)
	}
	if err := json.Unmarshal(text, &s); err != nil {
		return s, fmt.Errorf("the scan %s is not a scan: %w", path, err)
	}
	if !filepath.IsAbs(s.Store) {
		return s, fmt.Errorf("the scan %s names no store", path)
	}
	return s, nil
}

// podmanView is O5's podman half: every container (the external ones too), the named volumes
// each of podman's own mounts, and the volumes.
func (a App) podmanView(ctx context.Context) (podmanView, error) {
	pv := podmanView{containers: map[string]engine.StoreContainer{}, volumes: map[string]bool{}}
	eng, err := engine.New(engine.Podman, a.Env.Runner)
	if err != nil {
		return pv, err
	}
	cs, err := eng.StoreContainers(ctx)
	if err != nil {
		return pv, err
	}
	var own []string
	for _, c := range cs {
		pv.containers[c.ID] = c
		if !c.External {
			own = append(own, c.ID)
		}
	}
	if pv.mounts, err = eng.ContainerVolumes(ctx, own); err != nil {
		return pv, err
	}
	vols, err := eng.StoreVolumes(ctx)
	if err != nil {
		return pv, err
	}
	for _, v := range vols {
		pv.volumes[v.Name] = true
	}
	return pv, nil
}

// userItems is the items of root's scan, judged as the store's user with the old range given.
func (a App) userItems(ctx context.Context, sub, scanPath string, old Range) (Scan, records, []item, error) {
	var rec records
	if err := a.notRoot(sub); err != nil {
		return Scan{}, rec, nil, err
	}
	scan, err := readScan(scanPath)
	if err != nil {
		return scan, rec, nil, err
	}
	if err = a.asUser(scan.Owners.UID); err != nil {
		return scan, rec, nil, err
	}
	if rec, err = readRecords(scan.Store); err != nil {
		return scan, rec, nil, err
	}
	pv, err := a.podmanView(ctx)
	if err != nil {
		return scan, rec, nil, err
	}
	return scan, rec, triage(scan, rec, pv, old), nil
}

// presentNow is O11's question for each decided key that is no item now: does the store still
// have it. podman answers for an image, a container and a volume, c/storage's records for a
// layer, and the store's own directory for a path.
func (a App) presentNow(ctx context.Context, scan Scan, rec records, keys []itemKey) (map[itemKey]bool, error) {
	present := map[itemKey]bool{}
	eng, err := engine.New(engine.Podman, a.Env.Runner)
	if err != nil {
		return present, err
	}
	for _, k := range keys {
		switch k.kind {
		case kindLayer:
			_, present[k] = rec.parent[k.key]
		case kindPath:
			_, statErr := os.Lstat(filepath.Join(scan.Store, filepath.Clean("/"+k.key)))
			if statErr != nil && !errors.Is(statErr, fs.ErrNotExist) {
				return present, fmt.Errorf("cannot tell whether the store still has %s: %w", k.key, statErr)
			}
			present[k] = statErr == nil
		default:
			if present[k], err = eng.StoreHas(ctx, k.kind, k.key); err != nil {
				return present, err
			}
		}
	}
	return present, nil
}

// triageCmd is `old-range triage` (O14): the report on stdout, the proposal to proposed.
func (a App) triageCmd(ctx context.Context, scanPath string, old Range, proposed string) error {
	scan, _, items, err := a.userItems(ctx, "triage", scanPath, old)
	if err != nil {
		return err
	}
	if proposed != "" {
		if err := os.WriteFile(filepath.Clean(proposed), []byte(proposedDecisions(items, old)), 0o600); err != nil {
			return fmt.Errorf("cannot write the proposed decisions: %w", err)
		}
	}
	if _, err := a.Env.Stdout.Write([]byte(triageReport(scan, old, items))); err != nil {
		return fmt.Errorf("cannot write the report to stdout: %w", err)
	}
	return nil
}

// planCmd is `old-range plan` (O11): the decisions checked against the store now, the plan as
// JSON.
func (a App) planCmd(ctx context.Context, scanPath, decisionsPath string) error {
	if err := a.notRoot("plan"); err != nil {
		return err
	}
	text, err := os.ReadFile(filepath.Clean(decisionsPath))
	if err != nil {
		return fmt.Errorf("cannot read the decisions: %w", err)
	}
	d, err := parseDecisions(string(text))
	if err != nil {
		return err
	}
	scan, rec, items, err := a.userItems(ctx, "plan", scanPath, d.old)
	if err != nil {
		return err
	}
	present, err := a.presentNow(ctx, scan, rec, notItems(items, d))
	if err != nil {
		return err
	}
	p, err := makePlan(scan, items, d, present)
	if err != nil {
		return fmt.Errorf("the decisions are refused, nothing is planned:\n%w", err)
	}
	return a.printJSON(p)
}

// triageReport is the markdown a person (the host agent) decides from.
func triageReport(scan Scan, old Range, items []item) string {
	var b strings.Builder
	count := map[string]int{}
	for _, it := range items {
		count[it.proposal]++
	}
	fmt.Fprintf(&b, "# Old-range triage of %s\n\n", scan.Store)
	fmt.Fprintf(&b, "- user: %s (uid %d, gid %d)\n- current subuid range: %s; subgid range: %s\n- old range assumed: %s\n",
		scan.User, scan.Owners.UID, scan.Owners.GID, scan.Owners.SubUID, scan.Owners.SubGID, old)
	fmt.Fprintf(&b, "- old-range owners seen: %s\n\n", ownerSpan(scan.Dirs))
	fmt.Fprintf(&b, "%d items: %d REMOVE, %d RE-OWN, %d ASK, %d HELD\n", len(items),
		count[proposeRemove], count[proposeReown], count[proposeAsk], count[proposeHeld])
	for _, it := range items {
		fmt.Fprintf(&b, "\n## %s %s%s\n\n- proposed %s: %s\n- may be: %s\n", it.kind, it.key, namesOf(it), it.proposal, it.why, strings.Join(it.allowed, ", "))
		if len(it.users) > 0 {
			fmt.Fprintf(&b, "- used by: %s\n", strings.Join(it.users, "; "))
		}
		fmt.Fprintf(&b, "- old-range paths: %d in %s; everything under them: %s, %s\n", it.paths, strings.Join(it.dirs, ", "), humanBytes(it.size), dataWord(it.data))
		if len(it.held) > 0 {
			fmt.Fprintf(&b, "- held: %s\n", strings.Join(it.held, "; "))
		}
		if it.unshiftable {
			b.WriteString("- some owners lie outside the old range, or past the current range's size: cannot be shifted\n")
		}
		if len(it.samples) > 0 {
			fmt.Fprintf(&b, "- samples: %s\n", strings.Join(it.samples, ", "))
		}
	}
	return b.String()
}

func dataWord(data bool) string {
	if data {
		return "holds data"
	}
	return "holds no data"
}

// ownerSpan is the lowest and highest old-range uid and gid of the scan, the old range's evidence.
func ownerSpan(dirs []StoreDir) string {
	var uids, gids []int64
	for _, d := range dirs {
		uids = append(uids, d.UIDs...)
		gids = append(gids, d.GIDs...)
	}
	if len(uids) == 0 {
		return "none"
	}
	return "uids " + span(uids) + ", gids " + span(gids)
}

func span(ids []int64) string {
	return strconv.FormatInt(slices.Min(ids), 10) + "-" + strconv.FormatInt(slices.Max(ids), 10)
}
