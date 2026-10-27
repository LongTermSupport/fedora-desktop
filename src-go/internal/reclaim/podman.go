package reclaim

import (
	"context"
	"fmt"
	"os"
	"os/user"
	"regexp"
	"strconv"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
)

// titleImages is the listing the image prune of E22a chooses from.
const titleImages = "Images"

// podmanListings are what E7a and E22a read when `podman system df` could not read a volume:
// none of them opens a volume's data.
var podmanListings = []struct {
	title string
	cmd   execx.Cmd
}{
	{titleImages, execx.Cmd{Name: progPodman, Args: []string{"images"}}},
	{"Containers", execx.Cmd{Name: progPodman, Args: []string{"ps", "--all", "--size"}}},
	{"Volumes", execx.Cmd{Name: progPodman, Args: []string{"volume", "ls"}}},
}

// podmanImagePrune removes only images no container uses; it neither reads nor removes a volume.
var podmanImagePrune = execx.Cmd{Name: progPodman, Args: []string{"image", argPrune, argAllForce}}

// refusedData is the volume data podman says it was refused permission to open.
var refusedData = regexp.MustCompile(`open (\S*/volumes/([^/\s]+)/_data): permission denied`)

// refusedPermission is whether a failed podman read was refused permission.
func refusedPermission(text string) bool {
	return strings.Contains(text, "permission denied")
}

// listing is one of podmanListings as read.
type listing struct {
	title string
	cmd   execx.Cmd
	out   string
	err   error
}

func (l listing) heading() string {
	return "  " + l.title + " (" + l.cmd.Line() + "):"
}

func (r *run) readPodmanListings(ctx context.Context) []listing {
	out := make([]listing, 0, len(podmanListings))
	for _, l := range podmanListings {
		res, err := r.env.Runner.Run(ctx, l.cmd)
		out = append(out, listing{title: l.title, cmd: l.cmd, out: string(res.Stdout), err: err})
	}
	return out
}

// podmanPartialReport is E7a's section: what podman can list without opening a volume, labelled
// as partial, after the unreadable volume named; each listing that fails is named on stderr.
func (r *run) podmanPartialReport(ctx context.Context, refusal string) string {
	var b strings.Builder
	b.WriteString("  (partial: podman system df could not read every volume, so there are no totals)\n")
	for _, l := range r.unreadableVolume(refusal) {
		b.WriteString(l + "\n")
	}
	for _, l := range r.readPodmanListings(ctx) {
		b.WriteString(l.heading() + "\n")
		if l.err != nil {
			r.failf("%s: %v", l.cmd.Line(), l.err)
			b.WriteString("  (unavailable)\n")
			continue
		}
		if out := strings.TrimRight(l.out, "\n"); out != "" {
			b.WriteString(out + "\n")
		}
	}
	return b.String()
}

// podmanPartialPrune is E22a: podman's storage shown in part, and only the image prune offered,
// since `podman system prune` would remove stopped containers podman could not fully see.
func (r *run) podmanPartialPrune(ctx context.Context, out string) bool {
	r.warn("  Cannot query podman fully: podman system df could not read every volume. Reason:")
	r.show(out)
	for _, l := range r.unreadableVolume(out) {
		r.say(l)
	}
	r.info("  Podman storage, in part (no totals):")
	images := false
	for _, l := range r.readPodmanListings(ctx) {
		r.say(l.heading())
		if l.err != nil {
			r.say("    (unavailable: " + l.err.Error() + ")")
			continue
		}
		images = images || l.title == titleImages
		r.show(l.out)
	}
	if !images {
		r.warn("  No podman prune is offered: podman could not list its images either.")
		return true
	}
	r.warn("  podman system prune is not offered: podman could not read all of its storage, and the prune" +
		" removes stopped containers, which may be what still holds that volume.")
	if !r.confirm(ctx, "  Prune unused podman images ("+podmanImagePrune.Line()+")?") {
		r.say("  Skipped podman.")
		return true
	}
	if err := r.stream(finished(ctx), podmanImagePrune); err != nil {
		r.errLine("  podman image prune reported an error.")
		return false
	}
	r.okLine("  Podman images pruned.")
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
