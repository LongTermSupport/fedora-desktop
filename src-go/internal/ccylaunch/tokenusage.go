package ccylaunch

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/fsx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

// F11, `--token-usage [--json]`: the bash's usage_report (lib/token-management.bash), the
// usage of every token as a table or one JSON document, read from the API's rate-limit headers
// by one small request per account unless the cache holds a fresh answer.

const (
	usageJSONOption = "--json"
	usageCurl       = "curl"
	usagePrefix     = "ccy --token-usage: "
	usageFeedBefore = `header = "Authorization: Bearer `
	usageFeedAfter  = "\"\n"
	usageScaleFrac  = "fraction"
	curlHeader      = "--header"
	jsonNull        = "null"
	// maxUsageEpoch is the last second of the year 9999: a later reset the bash's date
	// prints otherwise, or not at all.
	maxUsageEpoch = 253402300799
	tempRandLen   = 10
	tempAlphabet  = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"
	minutesBelow  = 5400
	hoursBelow    = 172800
	secondsPerMin = 60
	secondsPerHr  = 3600
	fractionLimit = 2
	percentLimit  = 200
	percentOfOne  = 100
)

var (
	usageTTLText = regexp.MustCompile(`^[1-9][0-9]{0,17}$`)
	usageNumber  = regexp.MustCompile(`^[0-9]+(\.[0-9]+)?$`)
	usageEpoch   = regexp.MustCompile(`^[0-9]+$`)
	usageStatus  = regexp.MustCompile(`^[1-9][0-9]{2}$`)
	usageHeader  = regexp.MustCompile(`^[Aa]nthropic-[Rr]atelimit-unified-`)
	nonZeroDigit = regexp.MustCompile(`[1-9]`)
)

// usageSettings are the CCY_USAGE_* settings as the bash reads them, ${NAME:-default}.
type usageSettings struct {
	endpoint, model, timeout, connect, ttlText, scale string
	ttl                                               int64
	off                                               bool
}

// usageName is one row: a token name, the file a launch uses, the later files of the name it
// shadows, and the token, read when the file is one a launch takes.
type usageName struct {
	name, file, expires string
	shadows             []string
	valid               bool
	token               *execx.Secret
}

// usageRow is what the cache says of one name.
type usageRow struct {
	status, reason, p5, p7, r5, r7, claim, http, fetched string
	warn                                                 []string
}

// usageReport is one --token-usage run.
type usageReport struct {
	f                  Front
	s                  usageSettings
	dir, cacheDir, tmp string
	now                time.Time
	today, err         string
	json               bool
	names              []usageName
	rows               []usageRow
}

// tokenUsage is F11: its exit code, or why the bash should answer instead.
func (f Front) tokenUsage(ctx context.Context) (term.ExitCode, string) {
	r := usageReport{f: f}
	for _, arg := range f.Args[1:] {
		if arg != usageJSONOption {
			var p term.Printer
			p.Print(f.Stderr, usagePrefix+"unknown option "+arg+" (usage: ccy --token-usage [--json])\n")
			if p.Err() != nil {
				return term.ExitFailure, ""
			}
			return term.ExitUsage, ""
		}
		r.json = true
	}
	if why := r.prepare(); why != "" {
		return term.ExitFailure, why
	}
	if code, why := r.fetchAll(ctx); code != term.ExitOK || why != "" {
		return code, why
	}
	if why := r.readRows(); why != "" {
		return term.ExitFailure, why
	}
	return r.print(), ""
}

// fetchAll says what it is about to ask (in the table's run) and asks it: a failure's exit
// code, or why the bash should answer after all.
func (r *usageReport) fetchAll(ctx context.Context) (term.ExitCode, string) {
	fetch := r.toFetch()
	if len(fetch) == 0 {
		return term.ExitOK, ""
	}
	if !r.json {
		var p term.Printer
		p.Print(r.f.Stderr, fmt.Sprintf("Reading usage for %d account(s): 1 small API call each, unless read in the last %s seconds...\n", len(fetch), r.s.ttlText))
		if err := p.Err(); err != nil {
			return r.f.fail(usagePrefix + "could not write: " + err.Error()), ""
		}
	}
	why, err := r.prime(ctx, fetch)
	if err != nil {
		return r.f.fail(usagePrefix + err.Error()), ""
	}
	// After a hand-over here the bash prints the progress line again, and finds the entries
	// the front wrote fresh, so the API is not asked twice.
	return term.ExitOK, why
}

// usageSettings reads the settings, or why the bash could read them otherwise.
func (f Front) usageSettings() (usageSettings, string) {
	or := func(name, def string) string {
		if v := f.getenv(name); v != "" {
			return v
		}
		return def
	}
	s := usageSettings{
		endpoint: or("CCY_USAGE_ENDPOINT", "https://api.anthropic.com/v1/messages"),
		model:    or("CCY_USAGE_MODEL", "claude-haiku-4-5-20251001"),
		timeout:  or("CCY_USAGE_TIMEOUT", "20"),
		connect:  or("CCY_USAGE_CONNECT_TIMEOUT", "5"),
		ttlText:  or("CCY_USAGE_TTL", "900"),
		scale:    or("CCY_USAGE_SCALE", usageScaleFrac),
		off:      or("CCY_TOKEN_USAGE", "1") == "0",
	}
	for _, v := range []string{s.endpoint, s.model, s.timeout, s.connect, s.scale} {
		if !printableASCII(v) {
			return s, "a CCY_USAGE_ setting is not printable ASCII"
		}
	}
	// A leading nought is octal to the bash's printf; nought itself would have the bash fetch
	// again what the front hands it after a fetch.
	if !usageTTLText.MatchString(s.ttlText) {
		return s, "CCY_USAGE_TTL is not a number the front reads as the bash does"
	}
	ttl, err := strconv.ParseInt(s.ttlText, 10, 64)
	if err != nil {
		return s, "CCY_USAGE_TTL: " + err.Error()
	}
	s.ttl = ttl
	return s, ""
}

// prepare reads the settings and the pool, and checks every file the run will read, before
// anything is written: why the bash should answer, or "".
func (r *usageReport) prepare() string {
	s, why := r.f.usageSettings()
	if why != "" {
		return why
	}
	r.s = s
	if !zoneReadsAlike(r.f.getenv) {
		return "the local zone could be read otherwise"
	}
	if !printableASCII(r.f.getenv("HOME")) {
		return "HOME is not printable ASCII"
	}
	if why := r.tempDir(); why != "" {
		return why
	}
	r.now = r.f.clock().Now()
	r.today = r.now.Format(time.DateOnly)
	r.dir = r.f.tokenDir()
	r.cacheDir = filepath.Dir(r.dir) + "/usage-cache"
	switch {
	case s.off:
		r.err = "usage reading is turned off (CCY_TOKEN_USAGE=0)"
	case !r.f.found(usageCurl):
		r.err = "needs curl (run play-claude-yolo.yml)"
	case !isDir(r.dir):
		r.err = "no token directory at " + r.dir + " (create a token with: ccy --create-token)"
	default:
		if why := r.readNames(); why != "" {
			return why
		}
		if len(r.names) == 0 {
			r.err = "no tokens in " + r.dir + " (create one with: ccy --create-token)"
		}
	}
	return r.checkFetch()
}

// tempDir is mktemp's directory: TMPDIR, else /tmp.
func (r *usageReport) tempDir() string {
	r.tmp = r.f.getenv("TMPDIR")
	if r.tmp == "" {
		r.tmp = "/tmp"
	}
	if !filepath.IsAbs(r.tmp) || filepath.Clean(r.tmp) != r.tmp || !printableASCII(r.tmp) || !isDir(r.tmp) {
		return "TMPDIR is not a clean directory: " + r.tmp
	}
	return ""
}

func (f Front) found(tool string) bool {
	_, err := f.LookPath(tool)
	return err == nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// readNames is the pool as the bash groups it: one name per first file of the name, in glob
// order, and the later files of the name as its shadows.
func (r *usageReport) readNames() string {
	files, why := r.f.poolWhy(r.dir, r.now)
	if why != "" {
		return why
	}
	at := map[string]int{}
	for _, fn := range files {
		m := bashDate.FindStringSubmatch(fn)
		if m == nil {
			return "a token file name has no date: " + fn
		}
		if !printableASCII(fn) {
			return "a token file name is not printable ASCII"
		}
		name := bashName(fn)
		if i, seen := at[name]; seen {
			r.names[i].shadows = append(r.names[i].shadows, fn)
			continue
		}
		at[name] = len(r.names)
		r.names = append(r.names, usageName{name: name, file: fn, expires: m[1], valid: m[1] > r.today})
	}
	return ""
}

// checkFetch reads the token of every name a launch could use, and every cache entry the run
// reads: why the bash could read one otherwise, or "".
func (r *usageReport) checkFetch() string {
	if !r.anyValid() {
		return ""
	}
	if info, err := os.Lstat(r.cacheDir); err == nil && !info.IsDir() {
		return "the usage cache is not a directory"
	}
	for i := range r.names {
		n := &r.names[i]
		if !n.valid {
			continue
		}
		if why := r.readToken(n); why != "" {
			return why
		}
		if _, why := r.entry(n.name); why != "" {
			return why
		}
	}
	return ""
}

// readToken is $(cat "$token_file") as a secret (none when it is empty), or why the bash could
// read it otherwise or would put it in curl's argv.
func (r *usageReport) readToken(n *usageName) string {
	raw, err := os.ReadFile(filepath.Join(r.dir, n.file))
	if err != nil {
		return "a token file cannot be read: " + n.file
	}
	if bytes.IndexByte(raw, 0) >= 0 {
		return "a token file holds a NUL: " + n.file
	}
	token := strings.TrimRight(string(raw), "\n")
	if token == "" {
		return ""
	}
	for _, word := range []string{r.s.endpoint, r.s.model, r.s.timeout, r.s.connect, r.tmp} {
		if strings.Contains(word, token) {
			return "a curl argument would hold a token"
		}
	}
	secret, err := execx.NewSecret("CCY_USAGE_TOKEN", token)
	if err != nil {
		return err.Error()
	}
	n.token = &secret
	return ""
}

func (r *usageReport) anyValid() bool {
	for _, n := range r.names {
		if n.valid {
			return true
		}
	}
	return false
}

func (r *usageReport) toFetch() []int {
	var fetch []int
	for i, n := range r.names {
		if n.valid {
			fetch = append(fetch, i)
		}
	}
	return fetch
}

func (r *usageReport) entryPath(name string) string {
	return filepath.Join(r.cacheDir, name+".result")
}

// entry is the cache entry of name split into lines (nil when there is none), or why the
// bash could read it otherwise: not a regular file, or a byte outside printable ASCII.
func (r *usageReport) entry(name string) ([]string, string) {
	path := r.entryPath(name)
	info, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, ""
	}
	if err != nil || !info.Mode().IsRegular() {
		return nil, "a usage cache entry is not a regular file: " + name
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, "a usage cache entry cannot be read: " + name
	}
	if !cacheText(raw) {
		return nil, "a usage cache entry is not printable ASCII: " + name
	}
	return strings.Split(string(raw), "\n"), ""
}

// line is the i'th line, "" past the end, as ${entry[i]:-}.
func line(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

// prime is usage_prime_cache: every name of fetch whose entry is not fresh for its file is
// asked of the API, in parallel. why is set when an answer is one the front does not model;
// that entry is not written.
func (r *usageReport) prime(ctx context.Context, fetch []int) (string, error) {
	if err := os.MkdirAll(r.cacheDir, dirMode); err != nil {
		return "", err
	}
	if err := os.Chmod(r.cacheDir, dirMode); err != nil {
		return "", err
	}
	now := r.f.clock().Now()
	whys := make([]string, len(r.names))
	errs := make([]error, len(r.names))
	var wg sync.WaitGroup
	for _, i := range fetch {
		n := r.names[i]
		if r.fresh(n, now) || n.token == nil {
			continue
		}
		wg.Go(func() { whys[i], errs[i] = r.fetch(ctx, n) })
	}
	wg.Wait()
	if err := errors.Join(errs...); err != nil {
		return "", err
	}
	for _, why := range whys {
		if why != "" {
			return why, nil
		}
	}
	return "", nil
}

const (
	dirMode  = 0o700
	fileMode = 0o600
)

// fresh reports whether name's entry was read with this very file within the TTL.
func (r *usageReport) fresh(n usageName, now time.Time) bool {
	lines, why := r.entry(n.name)
	if why != "" || line(lines, 0) != n.file {
		return false
	}
	info, err := os.Stat(r.entryPath(n.name))
	if err != nil {
		return false
	}
	age := now.Unix() - r.f.clock().ModTime(info).Unix()
	return age >= 0 && age < r.s.ttl
}

// fetch is _usage_fetch_one and the worker's publication: one request, its status and figures
// written as name's entry.
func (r *usageReport) fetch(ctx context.Context, n usageName) (string, error) {
	hdr, err := makeTemp(r.tmp)
	if err != nil {
		return "", err
	}
	body, err := makeTemp(r.tmp)
	if err != nil {
		return "", errors.Join(err, fsx.RemoveIfPresent(hdr))
	}
	why, err := r.ask(ctx, n, hdr, body)
	return why, errors.Join(err, fsx.RemoveIfPresent(hdr), fsx.RemoveIfPresent(body))
}

func (r *usageReport) ask(ctx context.Context, n usageName, hdr, body string) (string, error) {
	data := `{"model":"` + r.s.model + `","max_tokens":1,"messages":[{"role":"user","content":"."}]}`
	args := []string{
		"--config", "-", "--silent", "--request", "POST",
		curlHeader, "content-type: application/json",
		curlHeader, "anthropic-version: 2023-06-01",
		curlHeader, "anthropic-beta: oauth-2025-04-20",
		"--data", data, "--dump-header", hdr,
		"--connect-timeout", r.s.connect, "--max-time", r.s.timeout,
		"--output", body, "--write-out", "%{http_code}", r.s.endpoint,
	}
	res, err := r.f.Runner.Run(ctx, execx.Cmd{
		Name: usageCurl, Args: args,
		Feed: &execx.Feed{Secret: *n.token, Before: usageFeedBefore, After: usageFeedAfter},
	})
	code := "000"
	switch {
	case errors.Is(err, execx.ErrSecretInArgv):
		return "", err
	case err == nil:
		// As $(...) gives it: NULs dropped, trailing newlines removed.
		code = strings.TrimRight(strings.ReplaceAll(string(res.Stdout), "\x00", ""), "\n")
	}
	raw, err := os.ReadFile(hdr)
	if err != nil {
		return "", err
	}
	record, ok := usageRecord(raw)
	if !ok {
		return "the API sent a rate-limit header the front does not read as the bash does", nil
	}
	content := n.file + "\n" + code + "\n" + record + "\n"
	path := r.entryPath(n.name)
	if err := fsx.WriteAtomic(path, "usage cache entry", []byte(content), fileMode); err != nil {
		return "", err
	}
	return "", os.Chmod(path, fileMode)
}

// makeTemp is mktemp: a new empty file of mode 0600 named tmp. and ten letters or digits.
func makeTemp(dir string) (string, error) {
	for {
		buf := make([]byte, tempRandLen)
		if _, err := rand.Read(buf); err != nil {
			return "", err
		}
		for i, b := range buf {
			buf[i] = tempAlphabet[int(b)%len(tempAlphabet)]
		}
		path := filepath.Join(dir, "tmp."+string(buf))
		file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_EXCL, fileMode)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return "", err
		}
		return path, file.Close()
	}
}

// usageRecord is _usage_extract: the figures of the dumped headers as one record,
// u5 TAB r5 TAB u7 TAB r7 TAB claim, "" when neither bucket was sent. ok is false for a
// rate-limit header holding a byte outside printable ASCII, which the bash's lower-casing
// and output read by the locale.
func usageRecord(raw []byte) (string, bool) {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\x00", ""), "\n")
	var u5, u7, r5, r7, claim string
	// Only lines a newline ends: read's last, unterminated line is not read.
	for _, l := range lines[:len(lines)-1] {
		l = strings.TrimSuffix(l, "\r")
		if !usageHeader.MatchString(l) {
			continue
		}
		if !cacheText([]byte(l)) {
			return "", false
		}
		name, value := l, l
		if at := strings.IndexByte(l, ':'); at >= 0 {
			name, value = l[:at], l[at+1:]
		}
		value = strings.TrimPrefix(value, " ")
		switch asciiLower(name) {
		case "anthropic-ratelimit-unified-5h-utilization":
			u5 = value
		case "anthropic-ratelimit-unified-7d-utilization":
			u7 = value
		case "anthropic-ratelimit-unified-5h-reset":
			r5 = value
		case "anthropic-ratelimit-unified-7d-reset":
			r7 = value
		case "anthropic-ratelimit-unified-representative-claim":
			claim = value
		}
	}
	if strings.ContainsAny(claim, "\t\n") {
		claim = ""
	}
	if u5 == "" && u7 == "" {
		return "", true
	}
	return strings.Join([]string{u5, r5, u7, r7, claim}, "\t"), true
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + ('a' - 'A')
		}
	}
	return string(b)
}

// printableASCII reports whether s is only printable ASCII, which every locale reads alike.
func printableASCII(s string) bool {
	for i := range len(s) {
		if s[i] < ' ' || s[i] > '~' {
			return false
		}
	}
	return true
}

// cacheText reports whether b is printable ASCII, tabs and newlines.
func cacheText(b []byte) bool {
	for _, c := range b {
		if (c < ' ' || c > '~') && c != '\t' && c != '\n' {
			return false
		}
	}
	return true
}

// readRows is _usage_report_row for every name, and the shadow warnings.
func (r *usageReport) readRows() string {
	ok := 0
	for _, n := range r.names {
		row, why := r.row(n)
		if why != "" {
			return why
		}
		if len(n.shadows) > 0 {
			if n.valid {
				row.warn = append(row.warn, "shadows a later file of this name, which ccy never uses: remove the one you do not want")
			} else {
				row.warn = append(row.warn, "SHADOWED RENEWAL: ccy launches with this unusable file, not the later one; remove "+n.file)
			}
		}
		if row.status == "ok" {
			ok++
		}
		r.rows = append(r.rows, row)
	}
	if r.err == "" && ok == 0 {
		r.err = "usage could not be read for any token"
	}
	return ""
}

func (r *usageReport) row(n usageName) (usageRow, string) {
	row := usageRow{status: "unavailable"}
	if !n.valid {
		if n.expires == r.today {
			row.reason = "token expires today, so ccy will not launch with it; renew: ccy --update-token=" + n.name
		} else {
			row.reason = "token expired on " + n.expires + "; renew: ccy --update-token=" + n.name
		}
		return row, ""
	}
	lines, why := r.entry(n.name)
	if why != "" {
		return row, why
	}
	if line(lines, 0) != n.file {
		row.reason = "no figures were fetched for this token file"
		return row, ""
	}
	info, err := os.Stat(r.entryPath(n.name))
	if err != nil {
		return row, "a usage cache entry went away: " + n.name
	}
	mtime := r.f.clock().ModTime(info).Unix()
	row.fetched = strconv.FormatInt(mtime, 10)
	code, record := line(lines, 1), line(lines, 2)
	if usageStatus.MatchString(code) {
		row.http = code
	}
	now := r.now.Unix()
	switch {
	case mtime < now && now-mtime >= r.s.ttl:
		row.reason = "this run's fetch did not complete, and the cached figures are out of date"
	case record == "":
		row.reason = usageStatusReason(code)
	default:
		if why := r.figures(&row, record); why != "" {
			return row, why
		}
	}
	return row, ""
}

// figures reads one record into the row: the percentages, the warnings, the resets.
func (r *usageReport) figures(row *usageRow, record string) string {
	f := strings.SplitN(record, "\t", 5)
	for len(f) < 5 {
		f = append(f, "")
	}
	u5, r5, u7, r7 := f[0], f[1], f[2], f[3]
	row.claim = strings.ReplaceAll(f[4], "\t", "")
	for _, b := range []struct {
		raw, label string
		pct        *string
	}{{u5, "5-hour", &row.p5}, {u7, "7-day", &row.p7}} {
		pct, warn := r.pct(b.raw)
		*b.pct = pct
		if warn != "" {
			row.warn = append(row.warn, b.label+": "+warn)
		}
	}
	if row.p5 != "" || row.p7 != "" {
		row.status = "ok"
	} else {
		row.reason = "the API sent no usable figures"
	}
	for _, reset := range []struct {
		raw string
		to  *string
	}{{r5, &row.r5}, {r7, &row.r7}} {
		if !usageEpoch.MatchString(reset.raw) {
			continue
		}
		if len(reset.raw) > 1 && reset.raw[0] == '0' {
			return "a reset time has a leading nought, which the bash's arithmetic reads as octal"
		}
		if e, err := strconv.ParseInt(reset.raw, 10, 64); err != nil || e > maxUsageEpoch {
			return "a reset time is past any date the bash prints"
		}
		*reset.to = reset.raw
	}
	return ""
}

// pct is _usage_report_pct: the percentage as the report prints it, or the warning.
func (r *usageReport) pct(raw string) (string, string) {
	if raw == "" {
		return "", "the API did not report it"
	}
	if !usageNumber.MatchString(raw) {
		return "", "the API sent a value that is not a number (" + raw + ")"
	}
	x, err := strconv.ParseFloat(raw, 64)
	if err != nil && !errors.Is(err, strconv.ErrRange) {
		return "", "the API sent a value that is not a number (" + raw + ")"
	}
	fraction := r.s.scale == usageScaleFrac
	limit := float64(percentLimit)
	if fraction {
		limit = fractionLimit
	}
	if x > limit {
		return "", "SCALE MISMATCH, the API sent " + raw + ", impossible under CCY_USAGE_SCALE=" + r.s.scale
	}
	if fraction {
		// awk's printf "%.4f", x * 100, read back.
		x, err = strconv.ParseFloat(fmt.Sprintf("%.4f", x*percentOfOne), 64)
		if err != nil {
			return "", "the API sent a value that is not a number (" + raw + ")"
		}
	}
	s := strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", x), "0"), ".")
	return s, ""
}

// usageStatusReason is _usage_status_reason.
func usageStatusReason(code string) string {
	switch code {
	case "000":
		return "could not reach the API"
	case "401", "403":
		return "this token was not authorised to read it"
	case "429":
		return "rate limited"
	case "200":
		return "the API reported no limits"
	}
	return "HTTP " + code
}

// print writes the report and is its exit code: 0 when any token's usage was read.
func (r *usageReport) print() term.ExitCode {
	var p term.Printer
	if r.json {
		p.Print(r.f.Stdout, r.jsonText())
	} else {
		p.Print(r.f.Stdout, r.table())
		if r.err != "" {
			p.Print(r.f.Stderr, usagePrefix+r.err+"\n")
		}
	}
	if err := p.Err(); err != nil {
		return r.f.fail(usagePrefix + "could not write the report: " + err.Error())
	}
	for _, row := range r.rows {
		if row.status == "ok" {
			return term.ExitOK
		}
	}
	return term.ExitFailure
}

func (r *usageReport) jsonText() string {
	var b strings.Builder
	b.WriteString("{\n  \"schema\": 1,\n  \"generated_at\": " + usageJSONTime(strconv.FormatInt(r.now.Unix(), 10)) +
		",\n  \"scale\": " + usageJSONString(r.s.scale) + ",\n  \"error\": " + usageJSONOrNull(r.err) + ",\n  \"tokens\": [")
	for i, n := range r.names {
		row := r.rows[i]
		if i > 0 {
			b.WriteString(",")
		}
		num := func(s string) string {
			if s == "" {
				return jsonNull
			}
			return s
		}
		fmt.Fprintf(&b, "\n    {\"name\": %s, \"file\": %s, \"expires\": %s, \"status\": %s, \"reason\": %s, \"five_hour_pct\": %s, \"seven_day_pct\": %s, \"resets\": {\"five_hour\": %s, \"seven_day\": %s}, \"binding_limit\": %s, \"http_status\": %s, \"fetched_at\": %s, \"warnings\": [%s], \"shadowed\": [%s]}",
			usageJSONString(n.name), usageJSONString(n.file), usageJSONOrNull(n.expires), usageJSONString(row.status),
			usageJSONOrNull(row.reason), num(row.p5), num(row.p7), usageJSONTime(row.r5), usageJSONTime(row.r7),
			usageJSONOrNull(row.claim), num(row.http), usageJSONTime(row.fetched), usageJSONList(row.warn), usageJSONList(n.shadows))
	}
	if len(r.names) > 0 {
		b.WriteString("\n  ]\n}\n")
	} else {
		b.WriteString("]\n}\n")
	}
	return b.String()
}

func usageJSONList(items []string) string {
	quoted := make([]string, 0, len(items))
	for _, s := range items {
		quoted = append(quoted, usageJSONString(s))
	}
	return strings.Join(quoted, ", ")
}

// usageJSONString is _usage_json_str: \ " newline, return and tab escaped, then every other
// control byte as \u00XX.
func usageJSONString(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for i := range len(s) {
		switch c := s[i]; c {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\r':
			b.WriteString(`\r`)
		case '\t':
			b.WriteString(`\t`)
		default:
			if c < ' ' || c == 0x7f {
				fmt.Fprintf(&b, `\u%04x`, c)
			} else {
				b.WriteByte(c)
			}
		}
	}
	b.WriteByte('"')
	return b.String()
}

func usageJSONOrNull(s string) string {
	if s == "" {
		return jsonNull
	}
	return usageJSONString(s)
}

// usageJSONTime is an epoch as an ISO-8601 UTC string, null when it is not one.
func usageJSONTime(epoch string) string {
	e, err := strconv.ParseInt(epoch, 10, 64)
	if !usageEpoch.MatchString(epoch) || err != nil {
		return jsonNull
	}
	return usageJSONString(time.Unix(e, 0).UTC().Format("2006-01-02T15:04:05Z"))
}

// table is _usage_report_print_table: one aligned row per name, then one per shadowed file.
func (r *usageReport) table() string {
	if len(r.names) == 0 {
		return ""
	}
	rows := [][]string{{"NAME", "EXPIRES", "5-HOUR", "RESETS", "7-DAY", "RESETS", "STATUS"}}
	for i, n := range r.names {
		row := r.rows[i]
		p5, t5 := r.bucketCells(row.p5, row.r5)
		p7, t7 := r.bucketCells(row.p7, row.r7)
		rows = append(rows, []string{n.name, n.expires, p5, t5, p7, t7, statusCell(row)})
		for _, sh := range n.shadows {
			sexp := "-"
			if m := bashDate.FindStringSubmatch(sh); m != nil {
				sexp = m[1]
			}
			rows = append(rows, []string{n.name, sexp, "-", "-", "-", "-", "shadowed: ccy --token " + n.name + " launches with " + n.file})
		}
	}
	width := make([]int, 6)
	for _, row := range rows {
		for c := range width {
			width[c] = max(width[c], len(row[c]))
		}
	}
	var b strings.Builder
	for _, row := range rows {
		fmt.Fprintf(&b, "%-*s  %-*s  %*s  %-*s  %*s  %-*s  %s\n",
			width[0], row[0], width[1], row[1], width[2], row[2], width[3], row[3],
			width[4], row[4], width[5], row[5], row[6])
	}
	return b.String()
}

// statusCell is a row's STATUS: ok and the binding limit, or why not; then the warnings.
func statusCell(row usageRow) string {
	status := "unavailable: " + row.reason
	if row.status == "ok" {
		status = "ok"
		if row.claim != "" {
			status += "; binding: " + usageClaimLabel(row.claim)
		}
	}
	if len(row.warn) > 0 {
		status += "; " + strings.Join(row.warn, "; ")
	}
	return status
}

// bucketCells are a bucket's percentage and reset cells, "-" for a bucket with no figure.
func (r *usageReport) bucketCells(pct, reset string) (string, string) {
	if pct == "" {
		return "-", "-"
	}
	return usagePctLabel(pct), r.resetText(reset)
}

// resetText is _usage_report_reset_text: "in 4 hours (2026-10-10 18:00 UTC)", or "-".
func (r *usageReport) resetText(reset string) string {
	e, err := strconv.ParseInt(reset, 10, 64)
	if !usageEpoch.MatchString(reset) || err != nil {
		return "-"
	}
	return usageHumanReset(e, r.now.Unix()) + " (" + time.Unix(e, 0).In(r.now.Location()).Format("2006-01-02 15:04 MST") + ")"
}

// usageHumanReset is _usage_human_reset less its "resets " (which the report drops): the time
// to a reset in words, to the nearest unit.
func usageHumanReset(epoch, now int64) string {
	delta := epoch - now
	if delta <= 0 {
		return "resetting now"
	}
	var n int64
	var unit string
	switch {
	case delta < minutesBelow:
		n, unit = (delta+secondsPerMin/2)/secondsPerMin, "minute"
	case delta < hoursBelow:
		n, unit = (delta+secondsPerHr/2)/secondsPerHr, "hour"
	default:
		n, unit = (delta+secondsPerDay/2)/secondsPerDay, "day"
	}
	if n != 1 {
		unit += "s"
	}
	return fmt.Sprintf("in %d %s", n, unit)
}

// usagePctLabel is _usage_pct_label: the percentage in four columns, "<1%" for a value above
// nought that rounds to it.
func usagePctLabel(v string) string {
	x, err := strconv.ParseFloat(v, 64)
	if err != nil {
		return v
	}
	p := strconv.FormatFloat(x, 'f', 0, 64)
	if p == "0" && nonZeroDigit.MatchString(v) {
		return fmt.Sprintf("%4s", "<1%")
	}
	return fmt.Sprintf("%3s%%", p)
}

// usageClaimLabel is _usage_claim_label.
func usageClaimLabel(claim string) string {
	switch claim {
	case "five_hour":
		return "5-hour limit"
	case "seven_day":
		return "weekly limit"
	case "seven_day_opus":
		return "weekly limit (Opus)"
	case "seven_day_sonnet":
		return "weekly limit (Sonnet)"
	}
	return claim
}
