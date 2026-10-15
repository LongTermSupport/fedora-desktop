// Package watermark is the watermark command: a visible two-layer mark (a corner annotation and
// a faint diagonal tile) and commercial-licence metadata on one JPEG or PNG image, through
// ImageMagick 7 and exiftool. The specification is CLAUDE/Plan/00192-.../SPEC-wsi-watermark.md;
// the expectation numbers (M1...) and departures from the bash (B21...) the comments cite are its.
package watermark

import (
	"bytes"
	"context"
	_ "embed" // usage.txt is embedded verbatim
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

//go:embed usage.txt
var usageText string

// The exit codes the usage text documents.
const (
	exitFailure  term.ExitCode = 1 // stdout could not be written (B30)
	exitUsage    term.ExitCode = 2
	exitSkip     term.ExitCode = 3
	exitMagick   term.ExitCode = 4
	exitExiftool term.ExitCode = 5
)

const (
	toolID                = "watermark/0.1.0"
	defaultSystemConfig   = "/etc/watermark/defaults.conf"
	defaultExiftoolConfig = "/etc/watermark/exiftool.config"
	magickBin             = "magick"
	exiftoolBin           = "exiftool"
)

// Env is everything a run touches outside its own memory and the files it names.
type Env struct {
	Runner   execx.Runner
	Stdout   io.Writer
	Stderr   io.Writer
	Getenv   func(string) string
	LookPath func(string) (string, error)
	Now      func() time.Time
	// SystemConfig is /etc/watermark/defaults.conf, the first config layer.
	SystemConfig string
}

// NewEnv is the production environment.
func NewEnv() Env {
	return Env{
		Runner: execx.OS{}, Stdout: os.Stdout, Stderr: os.Stderr, Getenv: os.Getenv,
		LookPath: exec.LookPath, Now: time.Now, SystemConfig: defaultSystemConfig,
	}
}

// App is one watermark invocation.
type App struct {
	Env Env
}

// failure ends a run with code after printing "watermark: <msg>".
type failure struct {
	code term.ExitCode
	msg  string
}

func (f *failure) Error() string { return f.msg }

func fail(code term.ExitCode, format string, args ...any) error {
	return &failure{code: code, msg: fmt.Sprintf(format, args...)}
}

// done ends a run early with code, its output already written (help, show-config, a skip).
type done struct{ code term.ExitCode }

func (d *done) Error() string { return fmt.Sprintf("done (%d)", d.code) }

// run is the state of one invocation.
type run struct {
	env     Env
	opts    options
	out     term.Printer
	errOut  term.Printer
	exifCfg string
}

// Run is one invocation; its exit code is M1-M14's.
func (a App) Run(ctx context.Context, args []string) term.ExitCode {
	r := &run{env: a.Env}
	code := r.result(r.main(ctx, args))
	if code == 0 && r.out.Err() != nil {
		r.say("could not write the output: " + r.out.Err().Error())
		return exitFailure
	}
	return code
}

func (r *run) result(err error) term.ExitCode {
	var f *failure
	var d *done
	switch {
	case err == nil:
		return term.ExitOK
	case errors.As(err, &d):
		return d.code
	case errors.As(err, &f):
		r.say(f.msg)
		return f.code
	default:
		r.say(err.Error())
		return exitUsage
	}
}

func (r *run) say(msg string) { r.errOut.Print(r.env.Stderr, "watermark: "+msg+"\n") }

func (r *run) verbose(msg string) {
	if r.opts.verbose {
		r.say(msg)
	}
}

func (r *run) main(ctx context.Context, args []string) error {
	opts, err := parseArgs(args)
	if err != nil {
		return fail(exitUsage, "%s", err.Error())
	}
	r.opts = opts
	if opts.help {
		r.out.Print(r.env.Stdout, usageText)
		return nil
	}
	s, err := r.settings()
	if err != nil {
		return err
	}
	if opts.showConfig {
		r.out.Print(r.env.Stdout, showConfig(s, opts))
		return nil
	}
	input, err := r.preflight(s)
	if err != nil {
		return err
	}
	return r.mark(ctx, s, input)
}

// preflight is M5-M8: one image, the tools, the settings and the image itself.
func (r *run) preflight(s Settings) (string, error) {
	switch n := len(r.opts.images); {
	case n == 0:
		r.errOut.Print(r.env.Stderr, usageText)
		return "", &done{code: exitUsage}
	case n > 1:
		return "", fail(exitUsage, "this version processes ONE image at a time; got %d (use 'find … -exec watermark {} \\;' for batch)", n)
	}
	if err := r.tools(); err != nil {
		return "", err
	}
	if r.opts.noTile && r.opts.noCorner {
		return "", fail(exitUsage, "--no-tile and --no-corner together leaves no visible watermark")
	}
	if err := s.checkRequired(); err != nil {
		return "", err
	}
	if err := s.checkPresentation(); err != nil {
		return "", fail(exitUsage, "%s", err.Error())
	}
	input := r.opts.images[0]
	return input, checkImage(input)
}

// tools is M6.
func (r *run) tools() error {
	if _, err := r.env.LookPath(magickBin); err != nil {
		return fail(exitMagick, "missing tool: magick (ImageMagick 7) — see Plan 00037")
	}
	if _, err := r.env.LookPath(exiftoolBin); err != nil {
		return fail(exitExiftool, "missing tool: exiftool (perl-Image-ExifTool)")
	}
	r.exifCfg = r.env.Getenv("WATERMARK_EXIFTOOL_CONFIG")
	if r.exifCfg == "" {
		r.exifCfg = defaultExiftoolConfig
	}
	if !isRegular(r.exifCfg) {
		return fail(exitExiftool, "missing exiftool config: %s (deploy via play-image-watermarking.yml)", r.exifCfg)
	}
	return nil
}

// mark is M9-M14 for an image that passed the checks.
func (r *run) mark(ctx context.Context, s Settings, input string) error {
	inputReal, err := resolve(input)
	if err != nil {
		return fail(exitUsage, "cannot resolve %s: %v", input, err)
	}
	if skip := r.skipIfMarked(ctx, input, inputReal); skip != nil {
		return skip
	}
	output := r.opts.output
	if output == "" {
		output = defaultOutput(input)
	}
	outputReal, err := resolve(output)
	if err != nil {
		return fail(exitUsage, "cannot resolve %s: %v", output, err)
	}
	if outputReal == inputReal {
		return fail(exitUsage, "--output resolves to the same path as input (%s); refusing to overwrite source", inputReal)
	}
	if _, err := os.Stat(output); err == nil {
		if !r.opts.force {
			return fail(exitUsage, "output already exists: %s (pass --force to overwrite)", output)
		}
		r.verbose("overwriting existing output: " + output)
	}
	magick := magickArgv(s, r.opts, input, output)
	exif := exiftoolArgv(s, r.exifCfg, output, r.env.Now().UTC().Format("2006:01:02 15:04:05Z"))
	if r.opts.dryRun {
		r.out.Print(r.env.Stdout, "# magick:\n"+joinArgv(magick)+"\n# exiftool:\n"+joinArgv(exif)+"\n")
		return nil
	}
	r.verbose("compose visible watermark → " + output)
	if err := r.tool(ctx, execx.Cmd{Name: magickBin, Args: magick[1:]}); err != nil {
		return fail(exitMagick, "magick failed (visible watermark)")
	}
	r.verbose("write metadata (EXIF/IPTC/XMP + XMP-wm sentinel)")
	if err := r.tool(ctx, execx.Cmd{Name: exiftoolBin, Args: exif[1:]}); err != nil {
		// The visible mark without the sentinel would be marked again by the next run (B26).
		if rmErr := os.Remove(output); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			r.say("could not remove the unmarked output: " + rmErr.Error())
		}
		return fail(exitExiftool, "exiftool failed (metadata write)")
	}
	r.out.Print(r.env.Stdout, outputReal+"\n")
	return nil
}

// tool runs one of the two engines with both of its streams on stderr (B25).
func (r *run) tool(ctx context.Context, c execx.Cmd) error {
	c.Stdout, c.Stderr = r.env.Stderr, r.env.Stderr
	_, err := r.env.Runner.Run(ctx, c)
	return err
}

// skipIfMarked is M9.
func (r *run) skipIfMarked(ctx context.Context, input, inputReal string) error {
	if r.opts.force {
		return nil
	}
	reason := ""
	if hasMarkedName(input) {
		reason = "filename suffix .watermarked.*"
	} else {
		var stdout bytes.Buffer
		cmd := execx.Cmd{Name: exiftoolBin, Args: []string{"-config", r.exifCfg, "-s3", "-XMP-wm:Applied", "--", input}, Stdout: &stdout}
		if _, err := r.env.Runner.Run(ctx, cmd); err != nil {
			return fail(exitExiftool, "exiftool could not read %s: %v", input, err)
		}
		if strings.TrimSpace(stdout.String()) == "True" {
			reason = "XMP-wm:Applied=True"
		}
	}
	if reason == "" {
		return nil
	}
	r.say("skip (" + reason + "): " + inputReal)
	r.out.Print(r.env.Stdout, inputReal+"\n")
	return &done{code: exitSkip}
}

var profileName = regexp.MustCompile(`^[A-Za-z0-9_-][A-Za-z0-9._-]*$`)

// settings is M2: the layers in order, then the command line.
func (r *run) settings() (Settings, error) {
	home := r.env.Getenv("HOME")
	if home == "" {
		return Settings{}, fail(exitUsage, "HOME is not set")
	}
	s := Defaults()
	layers := []struct {
		path     string
		required string // the error when the file is missing; empty for an optional layer
	}{
		{path: r.env.SystemConfig},
		{path: filepath.Join(home, ".config/watermark/defaults.conf")},
	}
	if p := r.opts.profile; p != "" {
		if !profileName.MatchString(p) {
			return s, fail(exitUsage, "--profile '%s' is not a profile name (letters, digits, '.', '_', '-')", p)
		}
		path := filepath.Join(home, ".config/watermark/profiles", p+".conf")
		layers = append(layers, struct{ path, required string }{path, fmt.Sprintf("--profile '%s' not found at %s", p, path)})
	}
	if c := r.opts.configFile; c != "" {
		layers = append(layers, struct{ path, required string }{c, "--config file not found: " + c})
	}
	for _, l := range layers {
		if !isRegular(l.path) {
			if l.required != "" {
				return s, fail(exitUsage, "%s", l.required)
			}
			continue
		}
		r.verbose("loading config: " + l.path)
		data, err := os.ReadFile(l.path)
		if err != nil {
			return s, fail(exitUsage, "cannot read %s: %v", l.path, err)
		}
		if err := ParseConfig(l.path, data, &s); err != nil {
			return s, fail(exitUsage, "%s", err.Error())
		}
	}
	r.opts.override(&s)
	return s, nil
}

// override applies the command line's values over the files', then the text default.
func (o options) override(s *Settings) {
	for _, v := range []struct {
		given *string
		into  *string
	}{
		{o.text, &s.Text},
		{o.artist, &s.Artist},
		{o.copyright, &s.Copyright},
		{o.licenceURL, &s.LicenceURL},
		{o.licenceSummary, &s.LicenceSummary},
	} {
		if v.given != nil {
			*v.into = *v.given
		}
	}
	if s.Text == "" {
		s.Text = s.Copyright
	}
}

// checkRequired is M7's semantic half.
func (s *Settings) checkRequired() error {
	for _, v := range []struct{ name, value string }{
		{"artist", s.Artist},
		{"copyright", s.Copyright},
		{"licence-url", s.LicenceURL},
		{"licence-summary", s.LicenceSummary},
		{"text", s.Text},
	} {
		if v.value == "" {
			return fail(exitUsage, "missing required value for '%s' (set via --%s or in a config file)", v.name, v.name)
		}
	}
	return nil
}

func isRegular(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}
