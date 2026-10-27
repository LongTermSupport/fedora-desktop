package reclaim

import (
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/user"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/rclonecache"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
)

const (
	argFormat  = "--format"
	formatJSON = "json"
	// summaryListed is how many of each kind E7a's summary lists.
	summaryListed = 10
	// yoloImages starts the name of every per-project claude-yolo image.
	yoloImages    = "localhost/claude-yolo:"
	shortIDLength = 12
	itemIndent    = "      "
)

// The listings E7a and E22a read when `podman system df` could not read a volume: none of them
// opens a volume's data.
var (
	podmanImages     = execx.Cmd{Name: progPodman, Args: []string{"images", argFormat, formatJSON}}
	podmanContainers = execx.Cmd{Name: progPodman, Args: []string{"ps", "--all", "--size", argFormat, formatJSON}}
	podmanVolumes    = execx.Cmd{Name: progPodman, Args: []string{"volume", "ls", argFormat, formatJSON}}
)

// podmanDanglingPrune removes only dangling images no container uses; podmanUnusedPrune every
// image no container uses, tagged ones included. Neither reads nor removes a volume.
var (
	podmanDanglingPrune = execx.Cmd{Name: progPodman, Args: []string{argImage, argPrune, argForce}}
	podmanUnusedPrune   = execx.Cmd{Name: progPodman, Args: []string{argImage, argPrune, argAllForce}}
)

// sweepKeepsTagged is said where the safe sweep leaves the question of every unused image out.
const sweepKeepsTagged = "  The safe sweep removes no tagged image; option 6 offers removing every unused one."

// refusedData is the volume data podman says it was refused permission to open.
var refusedData = regexp.MustCompile(`open (\S*/volumes/([^/\s]+)/_data): permission denied`)

// refusedPermission is whether a failed podman read was refused permission.
func refusedPermission(text string) bool {
	return strings.Contains(text, "permission denied")
}

// podmanImage is one entry of `podman images --format json`.
type podmanImage struct {
	// ID is podman's Id: json matches a field name in any case.
	ID         string
	Names      []string
	Size       int64
	Containers int
	Dangling   bool
}

func (i podmanImage) dangling() bool { return i.Dangling || len(i.Names) == 0 }

// prunable is whether `podman image prune -f` removes the image.
func (i podmanImage) prunable() bool { return i.dangling() && i.Containers == 0 }

func (i podmanImage) name() string {
	if len(i.Names) > 0 {
		return i.Names[0]
	}
	id := strings.TrimPrefix(i.ID, "sha256:")
	return "<none> " + id[:min(shortIDLength, len(id))]
}

func (i podmanImage) yolo() bool {
	return slices.ContainsFunc(i.Names, func(n string) bool { return strings.HasPrefix(n, yoloImages) })
}

// podmanContainer is one entry of `podman ps --all --size --format json`.
type podmanContainer struct {
	Names []string
	State string
	Size  *struct {
		RwSize int64 `json:"rwSize"`
	}
}

type podmanVolume struct{ Name string }

// readList is c's JSON list.
func readList[T any](ctx context.Context, runner execx.Runner, c execx.Cmd) ([]T, error) {
	res, err := runner.Run(ctx, c)
	if err != nil {
		return nil, err
	}
	var out []T
	if err := json.Unmarshal(res.Stdout, &out); err != nil {
		return nil, fmt.Errorf("its output is not a JSON list: %w", err)
	}
	return out, nil
}

// sized is one line of a summary: a size and what has it.
type sized struct {
	bytes int64
	label string
}

// sizedLines are items, largest first (equal sizes by label), one line each.
func sizedLines(items []sized) []string {
	items = slices.Clone(items)
	slices.SortStableFunc(items, func(a, b sized) int {
		if a.bytes != b.bytes {
			return cmp.Compare(b.bytes, a.bytes)
		}
		return strings.Compare(a.label, b.label)
	})
	out := make([]string, 0, len(items))
	for _, it := range items {
		out = append(out, fmt.Sprintf("%s%8s  %s", itemIndent, rclonecache.IEC(it.bytes), it.label))
	}
	return out
}

// firstListed is title (given how many are listed) and the first summaryListed of lines, with
// how many more there are.
func firstListed(title string, lines []string) []string {
	shown := lines[:min(summaryListed, len(lines))]
	out := append([]string{fmt.Sprintf(title, len(shown))}, shown...)
	if more := len(lines) - len(shown); more > 0 {
		out = append(out, fmt.Sprintf("    and %d more", more))
	}
	return out
}

// keptDangling is how many of images are dangling yet used by a container, which the prune keeps.
func keptDangling(images []podmanImage) int {
	return len(slices.DeleteFunc(slices.Clone(images), func(i podmanImage) bool { return !i.dangling() || i.prunable() }))
}

func imagesSummary(images []podmanImage) (string, []string) {
	if len(images) == 0 {
		return "0", nil
	}
	items := make([]sized, 0, len(images))
	dangling := 0
	for _, i := range images {
		items = append(items, sized{i.Size, i.name()})
		if i.dangling() {
			dangling++
		}
	}
	line := fmt.Sprintf("    Dangling (<none>): %d", dangling)
	if kept := keptDangling(images); kept > 0 {
		line += fmt.Sprintf(", %d of them used by a container and kept by the prune", kept)
	}
	head := fmt.Sprintf("%d, total size not known: podman system df could not run, and images share layers", len(images))
	return head, append([]string{line}, firstListed("    Largest %d:", sizedLines(items))...)
}

func containersSummary(containers []podmanContainer) (string, []string) {
	if len(containers) == 0 {
		return "0", nil
	}
	items := make([]sized, 0, len(containers))
	for _, c := range containers {
		var rw int64
		if c.Size != nil {
			rw = c.Size.RwSize
		}
		items = append(items, sized{rw, strings.Join(c.Names, ",") + " (" + c.State + ")"})
	}
	return strconv.Itoa(len(containers)), firstListed("    Largest %d by writable layer:", sizedLines(items))
}

func volumesSummary(volumes []podmanVolume) (string, []string) {
	if len(volumes) == 0 {
		return "0", nil
	}
	names := make([]string, 0, len(volumes))
	for _, v := range volumes {
		names = append(names, itemIndent+v.Name)
	}
	slices.Sort(names)
	return fmt.Sprintf("%d, not sized, since sizing a volume opens its data", len(volumes)),
		firstListed("    First %d by name:", names)
}

// podmanPart is one kind of podman storage as E7a summarises it, or why it could not be read.
type podmanPart struct {
	title   string
	cmd     execx.Cmd
	summary string
	lines   []string
	err     error
}

func (p podmanPart) heading() string {
	return "  " + p.title + " (" + p.cmd.Line() + "): "
}

func readPart[T any](ctx context.Context, r *run, title string, c execx.Cmd, sum func([]T) (string, []string)) ([]T, podmanPart) {
	list, err := readList[T](ctx, r.env.Runner, c)
	p := podmanPart{title: title, cmd: c, err: err}
	if err == nil {
		p.summary, p.lines = sum(list)
	}
	return list, p
}

// podmanInPart is E7a's summary of the images, containers and volumes, and the image list read
// (its part's err says why it could not be).
type podmanInPart struct {
	parts  []podmanPart
	images []podmanImage
}

func (s podmanInPart) imagesErr() error { return s.parts[0].err }

func (r *run) readInPart(ctx context.Context) podmanInPart {
	images, pi := readPart(ctx, r, "Images", podmanImages, imagesSummary)
	_, pc := readPart(ctx, r, "Containers", podmanContainers, containersSummary)
	_, pv := readPart(ctx, r, "Volumes", podmanVolumes, volumesSummary)
	return podmanInPart{parts: []podmanPart{pi, pc, pv}, images: images}
}

// podmanPartialReport is E7a's section: a summary of what podman can list without opening a
// volume, labelled as partial, after the unreadable volume named; each listing that fails is
// named on stderr.
func (r *run) podmanPartialReport(ctx context.Context, refusal string) string {
	var b strings.Builder
	b.WriteString("  (partial: podman system df could not read every volume, so there are no totals)\n")
	for _, l := range r.unreadableVolume(refusal) {
		b.WriteString(l + "\n")
	}
	for _, p := range r.readInPart(ctx).parts {
		if p.err != nil {
			r.failf("%s: %v", p.cmd.Line(), p.err)
			b.WriteString(p.heading() + "unavailable\n")
			continue
		}
		b.WriteString(p.heading() + p.summary + "\n")
		for _, l := range p.lines {
			b.WriteString(l + "\n")
		}
	}
	return b.String()
}

// podmanPartialPrune is E22a: podman's storage summarised in part, and only image prunes
// offered, since `podman system prune` would remove stopped containers podman could not fully
// see; removing every unused image is offered only outside the safe sweep (everything).
func (r *run) podmanPartialPrune(ctx context.Context, out string, everything bool) bool {
	r.warn("  Cannot query podman fully: podman system df could not read every volume. Reason:")
	r.show(out)
	for _, l := range r.unreadableVolume(out) {
		r.say(l)
	}
	r.info("  Podman storage, in part (no totals):")
	storage := r.readInPart(ctx)
	for _, p := range storage.parts {
		if p.err != nil {
			r.say(p.heading() + "unavailable (" + p.err.Error() + ")")
			continue
		}
		r.say(p.heading() + p.summary)
		for _, l := range p.lines {
			r.say(l)
		}
	}
	if storage.imagesErr() != nil {
		r.warn("  No podman prune is offered: podman could not list its images either.")
		return true
	}
	r.warn("  podman system prune is not offered: podman could not read all of its storage, and the prune" +
		" removes stopped containers, which may be what still holds that volume.")
	pruned, ok := r.pruneDangling(ctx, storage.images)
	if !ok || ctx.Err() != nil {
		return false
	}
	if !everything {
		r.dimLine(sweepKeepsTagged)
		return true
	}
	images := storage.images
	if pruned {
		images = slices.DeleteFunc(slices.Clone(images), podmanImage.prunable)
	}
	return r.offerUnused(ctx, images)
}

// pruneDangling offers `podman image prune -f` with what it frees; whether it ran, and false
// when it failed.
func (r *run) pruneDangling(ctx context.Context, images []podmanImage) (bool, bool) {
	dangling := slices.DeleteFunc(slices.Clone(images), func(i podmanImage) bool { return !i.prunable() })
	if kept := keptDangling(images); kept > 0 {
		r.say(fmt.Sprintf("  %d dangling podman image(s) are used by a container and are kept by the prune.", kept))
	}
	if len(dangling) == 0 {
		r.okLine("  No dangling podman image to prune.")
		return false, true
	}
	if !r.confirm(ctx, fmt.Sprintf("  Prune the %d dangling (<none>) podman image(s) no container uses (%s)?",
		len(dangling), podmanDanglingPrune.Line())) {
		r.say("  Skipped " + podmanDanglingPrune.Line() + ".")
		return false, true
	}
	if err := r.stream(finished(ctx), podmanDanglingPrune); err != nil {
		r.errLine("  podman image prune reported an error.")
		return false, false
	}
	r.okLine("  Dangling podman images pruned.")
	return true, true
}

// podmanUnused is E22's last podman question, asked of the image list as it is now.
func (r *run) podmanUnused(ctx context.Context) bool {
	images, err := readList[podmanImage](ctx, r.env.Runner, podmanImages)
	if err != nil {
		r.warn("  Removing every unused image is not offered: podman could not list its images (" + err.Error() + ").")
		return true
	}
	return r.offerUnused(ctx, images)
}

// offerUnused asks, default no, to remove every image no container uses, naming the tagged
// ones it would take and listing the claude-yolo project images among them.
func (r *run) offerUnused(ctx context.Context, images []podmanImage) bool {
	var unused, tagged []podmanImage
	var yolo []sized
	for _, i := range images {
		if i.Containers > 0 {
			continue
		}
		unused = append(unused, i)
		if i.dangling() {
			continue
		}
		tagged = append(tagged, i)
		if i.yolo() {
			yolo = append(yolo, sized{i.Size, i.name()})
		}
	}
	if len(tagged) == 0 {
		r.dimLine("  No other podman image is unused: nothing more to remove.")
		return true
	}
	r.warn(fmt.Sprintf("  %s removes every image no container uses, not only the dangling ones: here %d tagged"+
		" image(s); each is pulled or rebuilt when it is next needed.",
		podmanUnusedPrune.Line(), len(tagged)))
	if len(yolo) > 0 {
		r.say(fmt.Sprintf("  Among them, %d claude-yolo project image(s), each rebuilt on that project's next launch:", len(yolo)))
		for _, l := range sizedLines(yolo) {
			r.say(l)
		}
		if others := len(tagged) - len(yolo); others > 0 {
			r.say(fmt.Sprintf("  and %d other tagged image(s).", others))
		}
	}
	if !r.confirm(ctx, fmt.Sprintf("  Also remove all %d unused podman image(s), tagged ones included (%s)?",
		len(unused), podmanUnusedPrune.Line())) {
		r.say("  Skipped " + podmanUnusedPrune.Line() + ".")
		return true
	}
	if err := r.stream(finished(ctx), podmanUnusedPrune); err != nil {
		r.errLine("  podman image prune -a reported an error.")
		return false
	}
	r.okLine("  Unused podman images removed.")
	return true
}

// unreadableVolume is E7a's lines on the volume a refusal names: why it could not be read, as
// found, and how its owner can look at it without changing it.
func (r *run) unreadableVolume(refusal string) []string {
	m := refusedData.FindStringSubmatch(refusal)
	if m == nil {
		return []string{"  podman's refusal names no volume, so which one it could not read is not known."}
	}
	path, name := m[1], m[2]
	who, uid, err := r.env.Account()
	why := fmt.Sprintf("this account could not be named (%v).", err)
	if err == nil {
		why = r.whyUnreadable(path, who, uid)
	}
	lines := make([]string, 0, 5)
	lines = append(lines, "  Unreadable volume "+name+" ("+path+"): "+why,
		"  To look at it without changing anything:",
		"    "+shellword.Join([]string{progPodman, "volume", "inspect", name}),
		"    "+shellword.Join([]string{"sudo", "ls", "-lan", path}))
	if err == nil {
		pattern := "^(" + who + "|" + strconv.Itoa(uid) + "):"
		lines = append(lines, "    "+shellword.Join([]string{"grep", "-E", pattern, subUIDFile}))
	}
	return lines
}

const subUIDFile = "/etc/subuid"

// whyUnreadable is whose the data at path is, against this account and its subordinate ranges.
func (r *run) whyUnreadable(path, who string, uid int) string {
	owner, err := r.env.Owner(path)
	if err != nil {
		return fmt.Sprintf("its owner could not be read (%v).", err)
	}
	if owner == uid {
		return fmt.Sprintf("owned by this account (user ID %d), yet podman could not read it.", owner)
	}
	ranges, err := r.subUIDRanges(who, uid)
	if err != nil {
		return fmt.Sprintf("owned by user ID %d; whether that is in this account's subordinate ID range is unknown,"+
			" since %s could not be read (%v).", owner, subUIDFile, err)
	}
	if len(ranges) == 0 {
		return fmt.Sprintf("owned by user ID %d, and this account has no subordinate ID range in %s;"+
			" podman cannot read or size it.", owner, subUIDFile)
	}
	spans := make([]string, 0, len(ranges))
	for _, rg := range ranges {
		if owner >= rg.start && owner < rg.start+rg.count {
			return fmt.Sprintf("owned by user ID %d, inside this account's subordinate ID range,"+
				" yet podman could not read it in its user namespace either.", owner)
		}
		spans = append(spans, fmt.Sprintf("%d-%d", rg.start, rg.start+rg.count-1))
	}
	return fmt.Sprintf("owned by user ID %d, outside this account's current subordinate ID range (%s)"+
		" — probably left from an older setup; podman cannot read or size it.", owner, strings.Join(spans, ", "))
}

type idRange struct{ start, count int }

// subUIDRanges are this account's lines of /etc/subuid, named by user name or uid.
func (r *run) subUIDRanges(who string, uid int) ([]idRange, error) {
	raw, err := r.env.SubUIDs()
	if err != nil {
		return nil, err
	}
	var out []idRange
	for i, line := range strings.Split(string(raw), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		f := strings.Split(line, ":")
		var start, count int
		var errStart, errCount error
		if len(f) == 3 {
			start, errStart = strconv.Atoi(f[1])
			count, errCount = strconv.Atoi(f[2])
		}
		if len(f) != 3 || errStart != nil || errCount != nil {
			return nil, fmt.Errorf("line %d: %q is not name:start:count", i+1, line)
		}
		if f[0] == who || f[0] == strconv.Itoa(uid) {
			out = append(out, idRange{start: start, count: count})
		}
	}
	return out, nil
}

// ownerUID is the production Env.Owner.
func ownerUID(path string) (int, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return 0, err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return 0, fmt.Errorf("%s: its file information names no owner", path)
	}
	return int(st.Uid), nil
}

// account is the production Env.Account.
func account() (string, int, error) {
	u, err := user.Current()
	if err != nil {
		return "", 0, err
	}
	uid, err := strconv.Atoi(u.Uid)
	return u.Username, uid, err
}
