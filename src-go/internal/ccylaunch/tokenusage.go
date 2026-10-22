package ccylaunch

import (
	"bytes"
	"context"
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

// F11, `--token-usage [--json]`: the usage of every token as a table or one JSON document (the
// document wsi-claude-token parses), read from the API's rate-limit headers by one small
// request per account unless the cache (the format a bash launch reads too) holds a fresh
// answer.

const (
	usageJSONOption = "--json"
	usageCurl       = "curl"
	usagePrefix     = "ccy --token-usage: "
	usageFeedBefore = `header = "Authorization: Bearer `
	usageFeedAfter  = "\"\n"
	usageScaleFrac  = "fraction"
	curlHeader      = "--header"
	jsonNull        = "null"
	tempPrefix      = "tmp."
	// maxTempTries bounds the search for a temporary name that does not hold the token
	// (readToken has refused a token the fixed part of every name holds).
	maxTempTries  = 64
	minutesBelow  = 5400
	hoursBelow    = 172800
	secondsPerMin = 60
	secondsPerHr  = 3600
	secondsPerDay = 86400
	fractionLimit = 2
	percentLimit  = 200
	percentOfOne  = 100
)

var (
	usageTTLText = regexp.MustCompile(`^[0-9]+$`)
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

// tokenUsage is F11: its exit code, or why the bash should answer instead, decided before
// anything is written.
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
	if code := r.fetchAll(ctx); code != term.ExitOK {
		return code, ""
	}
	r.readRows()
	return r.print(), ""
}

// fetchAll says what it is about to ask (in the table's run) and asks it: a failure's exit code.
func (r *usageReport) fetchAll(ctx context.Context) term.ExitCode {
	fetch := r.toFetch()
	if len(fetch) == 0 {
		return term.ExitOK
	}
	if !r.json {
		var p term.Printer
		p.Print(r.f.Stderr, fmt.Sprintf("Reading usage for %d account(s): 1 small API call each, unless read in the last %s seconds...\n", len(fetch), r.s.ttlText))
		if err := p.Err(); err != nil {
			return r.f.fail(usagePrefix + "could not write: " + err.Error())
		}
	}
	if err := r.prime(ctx, fetch); err != nil {
		return r.f.fail(usagePrefix + err.Error())
	}
	return term.ExitOK
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
	ttl, err := strconv.ParseInt(s.ttlText, 10, 64)
	if !usageTTLText.MatchString(s.ttlText) || err != nil {
		return s, "CCY_USAGE_TTL is not a whole number of seconds"
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
	if !printableASCII(r.f.getenv("HOME")) {
		return "HOME is not printable ASCII"
	}
	r.tmp = r.f.getenv("TMPDIR")
	if r.tmp == "" {
		r.tmp = "/tmp"
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

func (f Front) found(tool string) bool {
	_, err := f.LookPath(tool)
	return err == nil
}

func isDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}

// readNames is the pool as the bash groups it: one name per first file of the name, in byte
// order, and the later files of the name as its shadows.
func (r *usageReport) readNames() string {
	files, why := poolWhy(r.dir)
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
	if len(r.toFetch()) == 0 {
		return ""
	}
	if info, err := os.Lstat(r.cacheDir); err == nil && !info.IsDir() {
		return "the usage cache is not a directory"
	}
	for _, i := range r.toFetch() {
		n := &r.names[i]
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
// read it otherwise, or why curl's argv could not be made without it: a token that curl's fixed
// arguments or the start of every temporary name hold (a junk file such as "on"), which the
// bash sends and the front cannot.
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
	start := filepath.Join(r.tmp, tempPrefix)
	for _, word := range r.curlArgs(start, start) {
		if strings.Contains(word, token) {
			return "a curl argument would hold the token of " + n.file
		}
	}
	secret, err := execx.NewSecret("CCY_USAGE_TOKEN", token)
	if err != nil {
		return err.Error()
	}
	n.token = &secret
	return ""
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
// asked of the API, in parallel.
func (r *usageReport) prime(ctx context.Context, fetch []int) error {
	if err := os.MkdirAll(r.cacheDir, dirMode); err != nil {
		return err
	}
	if err := os.Chmod(r.cacheDir, dirMode); err != nil {
		return err
	}
	now := r.f.clock().Now()
	errs := make([]error, len(r.names))
	var wg sync.WaitGroup
	for _, i := range fetch {
		n := r.names[i]
		if r.fresh(n, now) || n.token == nil {
			continue
		}
		wg.Go(func() { errs[i] = r.fetch(ctx, n) })
	}
	wg.Wait()
	return errors.Join(errs...)
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
// written as name's entry. A temporary name that happens to hold the token is refused by the
// runner before curl starts, and the request is made again with new names.
func (r *usageReport) fetch(ctx context.Context, n usageName) error {
	for range maxTempTries {
		err := r.fetchVia(ctx, n)
		if !errors.Is(err, execx.ErrSecretInArgv) {
			return err
		}
	}
	return errors.New("every temporary name held the token of " + n.file)
}

func (r *usageReport) fetchVia(ctx context.Context, n usageName) error {
	hdr, err := makeTemp(r.tmp)
	if err != nil {
		return err
	}
	body, err := makeTemp(r.tmp)
	if err != nil {
		return errors.Join(err, fsx.RemoveIfPresent(hdr))
	}
	return errors.Join(r.ask(ctx, n, hdr, body), fsx.RemoveIfPresent(hdr), fsx.RemoveIfPresent(body))
}

// makeTemp is a new empty file of mode 0600 in dir, named tmp. and digits (D6).
func makeTemp(dir string) (string, error) {
	file, err := os.CreateTemp(dir, tempPrefix)
	if err != nil {
		return "", err
	}
	return file.Name(), file.Close()
}

// curlArgs is curl's argv: the settings, the header and body files, and never the token.
func (r *usageReport) curlArgs(hdr, body string) []string {
	data := `{"model":"` + r.s.model + `","max_tokens":1,"messages":[{"role":"user","content":"."}]}`
	return []string{
		"--config", "-", "--silent", "--request", "POST",
		curlHeader, "content-type: application/json",
		curlHeader, "anthropic-version: 2023-06-01",
		curlHeader, "anthropic-beta: oauth-2025-04-20",
		"--data", data, "--dump-header", hdr,
		"--connect-timeout", r.s.connect, "--max-time", r.s.timeout,
		"--output", body, "--write-out", "%{http_code}", r.s.endpoint,
	}
}

func (r *usageReport) ask(ctx context.Context, n usageName, hdr, body string) error {
	res, err := r.f.Runner.Run(ctx, execx.Cmd{
		Name: usageCurl, Args: r.curlArgs(hdr, body),
		Feed: &execx.Feed{Secret: *n.token, Before: usageFeedBefore, After: usageFeedAfter},
	})
	code := "000"
	switch {
	case errors.Is(err, execx.ErrSecretInArgv):
		return err
	case err == nil:
		// As $(...) gives it: NULs dropped, trailing newlines removed.
		code = strings.TrimRight(strings.ReplaceAll(string(res.Stdout), "\x00", ""), "\n")
	}
	raw, err := os.ReadFile(hdr)
	if err != nil {
		return err
	}
	content := n.file + "\n" + code + "\n" + usageRecord(raw) + "\n"
	path := r.entryPath(n.name)
	if err := fsx.WriteAtomic(path, "usage cache entry", []byte(content), fileMode); err != nil {
		return err
	}
	return os.Chmod(path, fileMode)
}

// usageRecord is _usage_extract: the figures of the dumped headers as one record,
// u5 TAB r5 TAB u7 TAB r7 TAB claim, "" when neither bucket was sent. A rate-limit header
// holding a byte outside printable ASCII is not read (D9).
func usageRecord(raw []byte) string {
	lines := strings.Split(strings.ReplaceAll(string(raw), "\x00", ""), "\n")
	var u5, u7, r5, r7, claim string
	// Only lines a newline ends: read's last, unterminated line is not read.
	for _, l := range lines[:len(lines)-1] {
		l = strings.TrimSuffix(l, "\r")
		if !usageHeader.MatchString(l) || !cacheText([]byte(l)) {
			continue
		}
		name, value := l, l
		if at := strings.IndexByte(l, ':'); at >= 0 {
			name, value = l[:at], l[at+1:]
		}
		value = strings.TrimPrefix(value, " ")
		switch strings.ToLower(name) {
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
		return ""
	}
	return strings.Join([]string{u5, r5, u7, r7, claim}, "\t")
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
func (r *usageReport) readRows() {
	ok := 0
	for _, n := range r.names {
		row := r.row(n)
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
}

func (r *usageReport) row(n usageName) usageRow {
	row := usageRow{status: "unavailable"}
	if !n.valid {
		if n.expires == r.today {
			row.reason = "token expires today, so ccy will not launch with it; renew: ccy --update-token=" + n.name
		} else {
			row.reason = "token expired on " + n.expires + "; renew: ccy --update-token=" + n.name
		}
		return row
	}
	lines, why := r.entry(n.name)
	info, err := os.Stat(r.entryPath(n.name))
	if why != "" || err != nil || line(lines, 0) != n.file {
		row.reason = "no figures were fetched for this token file"
		return row
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
		r.figures(&row, record)
	}
	return row
}

// figures reads one record into the row: the percentages, the warnings, the resets.
func (r *usageReport) figures(row *usageRow, record string) {
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
	if usageEpoch.MatchString(r5) {
		row.r5 = r5
	}
	if usageEpoch.MatchString(r7) {
		row.r7 = r7
	}
}

// pct is _usage_report_pct: the percentage as the report prints it, or the warning.
func (r *usageReport) pct(raw string) (string, string) {
	if raw == "" {
		return "", "the API did not report it"
	}
	x, err := strconv.ParseFloat(raw, 64)
	if !usageNumber.MatchString(raw) || err != nil && !errors.Is(err, strconv.ErrRange) {
		return "", "the API sent a value that is not a number (" + raw + ")"
	}
	limit := float64(percentLimit)
	if r.s.scale == usageScaleFrac {
		limit = fractionLimit
	}
	if x > limit {
		return "", "SCALE MISMATCH, the API sent " + raw + ", impossible under CCY_USAGE_SCALE=" + r.s.scale
	}
	if r.s.scale == usageScaleFrac {
		x *= percentOfOne
	}
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", x), "0"), "."), ""
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
