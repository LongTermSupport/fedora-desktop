package watermark

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/execx"
	"github.com/LongTermSupport/fedora-desktop/src-go/internal/term"
)

var (
	jpegBytes = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0, 0x10, 'J', 'F', 'I', 'F'}
	pngBytes  = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n', 0, 0, 0, 0x0D}
)

// kit is one watermark run's world: a temporary home and image directory, an exiftool config
// file, a fake runner on which magick and exiftool succeed, and the two streams.
type kit struct {
	t       *testing.T
	dir     string
	home    string
	sysConf string
	exifCfg string
	env     map[string]string
	missing []string
	run     *execx.Fake
	out     bytes.Buffer
	errb    bytes.Buffer
	stdout  io.Writer
}

func newKit(t *testing.T) *kit {
	t.Helper()
	dir := t.TempDir()
	k := &kit{
		t: t, dir: dir, home: filepath.Join(dir, "home"),
		sysConf: filepath.Join(dir, "etc", "defaults.conf"), exifCfg: filepath.Join(dir, "etc", "exiftool.config"),
		run: &execx.Fake{},
	}
	k.write(k.exifCfg, "%Image::ExifTool::UserDefined = ();\n")
	if err := os.MkdirAll(k.home, 0o750); err != nil {
		t.Fatal(err)
	}
	k.env = map[string]string{"HOME": k.home, "WATERMARK_EXIFTOOL_CONFIG": k.exifCfg}
	k.run.On([]string{"exiftool", "-config", k.exifCfg, "-s3"}, execx.Result{})
	k.run.OnFunc([]string{"magick"}, func(c execx.Cmd) (execx.Result, error) {
		out := c.Args[len(c.Args)-1]
		if err := os.WriteFile(out, []byte("marked"), 0o600); err != nil {
			return execx.Result{}, err
		}
		return execx.Result{}, nil
	})
	k.run.On([]string{"exiftool", "-config", k.exifCfg, "-overwrite_original"}, execx.Result{Stdout: []byte("    1 image files updated\n")})
	k.stdout = &k.out
	return k
}

func (k *kit) write(path, text string) {
	k.t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		k.t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		k.t.Fatal(err)
	}
}

// image writes an image file whose content starts with magic and returns its path.
func (k *kit) image(name string, magic []byte) string {
	k.t.Helper()
	path := filepath.Join(k.dir, "pics", name)
	k.write(path, string(magic))
	return path
}

// semantic is a user config with all five required values.
func (k *kit) semantic() {
	k.write(filepath.Join(k.home, ".config/watermark/defaults.conf"),
		"artist=\"Jane Doe\"\ncopyright=\"Jane Doe\"\nlicence_url=https://example.com/l\nlicence_summary='All rights reserved'\n")
}

func (k *kit) app() App {
	return App{Env: Env{
		Runner: k.run,
		Stdout: k.stdout,
		Stderr: &k.errb,
		Getenv: func(key string) string { return k.env[key] },
		LookPath: func(name string) (string, error) {
			if slices.Contains(k.missing, name) {
				return "", errors.New("not found")
			}
			return "/usr/bin/" + name, nil
		},
		Now:          func() time.Time { return time.Date(2026, 10, 15, 12, 34, 56, 0, time.UTC) },
		SystemConfig: k.sysConf,
	}}
}

func (k *kit) exec(args ...string) term.ExitCode {
	k.t.Helper()
	return k.app().Run(context.Background(), args)
}

func (k *kit) wantErr(code, want term.ExitCode, text string) {
	k.t.Helper()
	if code != want {
		k.t.Fatalf("exit %d, want %d; stderr %q", code, want, k.errb.String())
	}
	if !strings.Contains(k.errb.String(), "watermark: "+text+"\n") {
		k.t.Fatalf("stderr %q lacks %q", k.errb.String(), text)
	}
}

func TestM1HelpPrintsTheUsageOnStdout(t *testing.T) {
	k := newKit(t)
	if code := k.exec("--help"); code != 0 || k.out.String() != usageText || k.errb.Len() != 0 {
		t.Fatalf("exit %d, out %q, err %q", code, k.out.String(), k.errb.String())
	}
}

func TestM1AnUnknownOptionIsExit2(t *testing.T) {
	k := newKit(t)
	k.wantErr(k.exec("--frob"), 2, "unknown option: --frob (try --help)")
}

func TestM2EachLayerOverridesTheOneBeforeAndTheCommandLineWinsLast(t *testing.T) {
	k := newKit(t)
	k.write(k.sysConf, "artist=sys\ncopyright=sys\nlicence_url=sys\nquality=10\n")
	k.write(filepath.Join(k.home, ".config/watermark/defaults.conf"), "artist=user\ncopyright=user\nquality=20\n")
	k.write(filepath.Join(k.home, ".config/watermark/profiles/blog.conf"), "artist=profile\nquality=30\n")
	conf := filepath.Join(k.dir, "extra.conf")
	k.write(conf, "artist=config\n")
	if code := k.exec("--profile", "blog", "--config", conf, "--copyright", "", "-v", "--show-config"); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	got := k.out.String()
	for _, want := range []string{"\nartist=config\n", "\ncopyright=''\n", "\nlicence_url=sys\n", "\nquality=30\n", "\ntext=''\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("show-config lacks %q:\n%s", want, got)
		}
	}
	for _, p := range []string{k.sysConf, "profiles/blog.conf", conf} {
		if !strings.Contains(k.errb.String(), "watermark: loading config: ") || !strings.Contains(k.errb.String(), p) {
			t.Errorf("verbose did not name %s: %q", p, k.errb.String())
		}
	}
}

func TestM4ShowConfigPrintsTheResolvedValuesBeforeAnyCheck(t *testing.T) {
	k := newKit(t)
	k.missing = []string{"magick", "exiftool"}
	k.semantic()
	if code := k.exec("--show-config", "--no-tile", "-o", "out file.jpg", "a.jpg", "b.jpg"); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	want := `# resolved config (post-precedence-chain)
artist='Jane Doe'
copyright='Jane Doe'
licence_url=https://example.com/l
licence_summary='All rights reserved'
text='Jane Doe'
font=DejaVu-Sans-Bold
opacity_corner=0.85
opacity_tile=0.10
tile_angle=-30
size_pct_corner=0.030
size_pct_tile=0.018
quality=95
output='out file.jpg'
profile=''
config_file=''
no_tile=1
no_corner=0
`
	if k.out.String() != want {
		t.Fatalf("got\n%s", k.out.String())
	}
}

func TestM2AMissingProfileConfigOrHomeIsExit2(t *testing.T) {
	k := newKit(t)
	k.wantErr(k.exec("--profile", "blog", "x.jpg"), 2, "--profile 'blog' not found at "+filepath.Join(k.home, ".config/watermark/profiles/blog.conf"))
	k = newKit(t)
	k.wantErr(k.exec("--profile", "../escape", "x.jpg"), 2, "--profile '../escape' is not a profile name (letters, digits, '.', '_', '-')")
	k = newKit(t)
	k.wantErr(k.exec("--config", "/no/such.conf", "x.jpg"), 2, "--config file not found: /no/such.conf")
	k = newKit(t)
	delete(k.env, "HOME")
	k.wantErr(k.exec("x.jpg"), 2, "HOME is not set")
}

func TestM3ABadConfigLineIsExit2NamingTheFile(t *testing.T) {
	k := newKit(t)
	k.write(k.sysConf, "artist=$(id)\n")
	k.wantErr(k.exec("x.jpg"), 2, k.sysConf+":1: a bare value may not hold '$': quote it")
}

func TestM5NoImageIsTheUsageOnStderrAndTwoAreRefused(t *testing.T) {
	k := newKit(t)
	if code := k.exec(); code != 2 || k.errb.String() != usageText || k.out.Len() != 0 {
		t.Fatalf("exit %d, err %q", code, k.errb.String())
	}
	k = newKit(t)
	k.wantErr(k.exec("a.jpg", "b.jpg"), 2, "this version processes ONE image at a time; got 2 (use 'find … -exec watermark {} \\;' for batch)")
}

func TestM6MissingToolsAndConfigHaveTheirOwnCodes(t *testing.T) {
	k := newKit(t)
	k.missing = []string{"magick"}
	k.wantErr(k.exec("a.jpg"), 4, "missing tool: magick (ImageMagick 7) — see Plan 00037")
	k = newKit(t)
	k.missing = []string{"exiftool"}
	k.wantErr(k.exec("a.jpg"), 5, "missing tool: exiftool (perl-Image-ExifTool)")
	k = newKit(t)
	k.env["WATERMARK_EXIFTOOL_CONFIG"] = "/no/exif.config"
	k.wantErr(k.exec("a.jpg"), 5, "missing exiftool config: /no/exif.config (deploy via play-image-watermarking.yml)")
}

func TestM7ContradictionsAndMissingOrBadValuesAreExit2(t *testing.T) {
	k := newKit(t)
	k.semantic()
	k.wantErr(k.exec("--no-tile", "--no-corner", "a.jpg"), 2, "--no-tile and --no-corner together leaves no visible watermark")
	k = newKit(t)
	k.wantErr(k.exec("a.jpg"), 2, "missing required value for 'artist' (set via --artist or in a config file)")
	k = newKit(t)
	k.semantic()
	k.wantErr(k.exec("--licence-summary", "", "a.jpg"), 2, "missing required value for 'licence-summary' (set via --licence-summary or in a config file)")
	k = newKit(t)
	k.semantic()
	k.write(k.sysConf, "quality=200\n")
	k.wantErr(k.exec("a.jpg"), 2, "quality must be a whole number from 1 to 100, got '200'")
}

func TestM8TheImageMustBeAJPEGOrPNGFile(t *testing.T) {
	k := newKit(t)
	k.semantic()
	k.wantErr(k.exec(filepath.Join(k.dir, "none.jpg")), 2, "no such file: "+filepath.Join(k.dir, "none.jpg"))
	gif := k.image("x.gif", []byte("GIF89a"))
	k.wantErr(k.exec(gif), 2, "unsupported extension (JPEG and PNG only): "+gif)
	fake := k.image("fake.PNG", []byte("not a picture"))
	k.wantErr(k.exec(fake), 2, "input content is not a JPEG or PNG image: "+fake)
	swapped := k.image("swapped.jpg", pngBytes)
	k.errb.Reset()
	if code := k.exec("--dry-run", swapped); code != 0 {
		t.Fatalf("PNG content under a .jpg name is still an image: exit %d %s", code, k.errb.String())
	}
}

func TestM9AlreadyWatermarkedIsExit3WithThePathOnStdout(t *testing.T) {
	k := newKit(t)
	k.semantic()
	done := k.image("a.watermarked.JPG", jpegBytes)
	if code := k.exec(done); code != 3 || k.out.String() != done+"\n" {
		t.Fatalf("exit %d, out %q", code, k.out.String())
	}
	if k.errb.String() != "watermark: skip (filename suffix .watermarked.*): "+done+"\n" {
		t.Fatalf("stderr %q", k.errb.String())
	}

	k = newKit(t)
	k.semantic()
	img := k.image("b.png", pngBytes)
	k.run.On([]string{"exiftool", "-config", k.exifCfg, "-s3", "-XMP-wm:Applied", "--", img}, execx.Result{Stdout: []byte("True\n")})
	if code := k.exec(img); code != 3 || k.out.String() != img+"\n" || !strings.Contains(k.errb.String(), "skip (XMP-wm:Applied=True)") {
		t.Fatalf("exit %d, out %q, err %q", code, k.out.String(), k.errb.String())
	}
	k.out.Reset()
	if code := k.exec("--force", img); code != 0 {
		t.Fatalf("--force passes the probes: exit %d %s", code, k.errb.String())
	}
}

func TestM9AProbeThatFailsIsExit5(t *testing.T) {
	k := newKit(t)
	k.semantic()
	img := k.image("b.png", pngBytes)
	k.run.On([]string{"exiftool", "-config", k.exifCfg, "-s3"}, execx.Result{ExitCode: 1, Stderr: []byte("Error: bad file")})
	k.wantErr(k.exec(img), 5, "exiftool could not read "+img+": exiftool exited 1: Error: bad file")
}

func TestM10TheOutputIsBesideTheImageAndNeverTheImageOrAnExistingFile(t *testing.T) {
	k := newKit(t)
	k.semantic()
	img := k.image("Photo.JPEG", jpegBytes)
	if code := k.exec(img); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	out := filepath.Join(k.dir, "pics", "Photo.watermarked.jpeg")
	if k.out.String() != out+"\n" {
		t.Fatalf("out %q", k.out.String())
	}
	k.errb.Reset()
	k.wantErr(k.exec(img), 2, "output already exists: "+out+" (pass --force to overwrite)")
	k.errb.Reset()
	if code := k.exec("--force", "-v", img); code != 0 || !strings.Contains(k.errb.String(), "watermark: overwriting existing output: "+out) {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	link := filepath.Join(k.dir, "link.jpg")
	if err := os.Symlink(img, link); err != nil {
		t.Fatal(err)
	}
	k.wantErr(k.exec("-o", link, img), 2, "--output resolves to the same path as input ("+img+"); refusing to overwrite source")
}

func TestM11ToM13DryRunPrintsBothCommandsAfterEveryCheck(t *testing.T) {
	k := newKit(t)
	k.semantic()
	img := k.image("p.jpg", jpegBytes)
	out := filepath.Join(k.dir, "pics", "p.watermarked.jpg")
	if code := k.exec("--dry-run", img); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	want := "# magick:\n" +
		"magick " + img + " '(' -size 400x400 xc:none -font DejaVu-Sans-Bold -pointsize '%[fx:w*0.018]' " +
		"-fill 'rgba(255,255,255,0.10)' -stroke 'rgba(0,0,0,0.10)' -strokewidth 1 -gravity center -annotate -30 'Jane Doe' " +
		"-write mpr:tile +delete ')' '(' +clone -fill mpr:tile -draw 'color 0,0 reset' ')' -compose over -composite " +
		"-font DejaVu-Sans-Bold -pointsize '%[fx:w*0.030]' -fill 'rgba(255,255,255,0.85)' -stroke 'rgba(0,0,0,0.85)' " +
		"-strokewidth 2 -gravity southeast -annotate +30+30 'Jane Doe' -quality 95 " + out + "\n" +
		"# exiftool:\n" +
		"exiftool -config " + k.exifCfg + " -overwrite_original '-EXIF:Artist=Jane Doe' '-EXIF:Copyright=Jane Doe' " +
		"'-IPTC:By-line=Jane Doe' '-IPTC:CopyrightNotice=Jane Doe' '-XMP-dc:Creator=Jane Doe' '-XMP-dc:Rights=Jane Doe' " +
		"-XMP-xmpRights:Marked=True '-XMP-xmpRights:UsageTerms=All rights reserved' " +
		"-XMP-xmpRights:WebStatement=https://example.com/l -XMP-wm:Applied=True '-XMP-wm:AppliedAt=2026:10:15 12:34:56Z' " +
		"-XMP-wm:AppliedBy=watermark/0.1.0 " + out + "\n"
	if k.out.String() != want {
		t.Fatalf("got\n%s\nwant\n%s", k.out.String(), want)
	}
	if k.run.Called("magick") || k.run.Called("exiftool -config "+k.exifCfg+" -overwrite_original") {
		t.Fatal("a dry run ran a tool")
	}
}

func TestM11OneLayerLeavesTheOtherOut(t *testing.T) {
	k := newKit(t)
	k.semantic()
	img := k.image("p.png", pngBytes)
	if code := k.exec("--dry-run", "--no-tile", "--text", "T", img); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if strings.Contains(k.out.String(), "mpr:tile") || !strings.Contains(k.out.String(), "-annotate +30+30 T -quality") {
		t.Fatalf("--no-tile: %s", k.out.String())
	}
	k.out.Reset()
	if code := k.exec("--dry-run", "--no-corner", img); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if strings.Contains(k.out.String(), "southeast") || !strings.Contains(k.out.String(), "-composite -quality 95") {
		t.Fatalf("--no-corner: %s", k.out.String())
	}
}

func TestM14RunsMagickThenExiftoolAndPrintsTheOutputPath(t *testing.T) {
	k := newKit(t)
	k.semantic()
	img := k.image("p.jpg", jpegBytes)
	if code := k.exec("-v", "-o", filepath.Join(k.dir, "o.jpg"), img); code != 0 {
		t.Fatalf("exit %d: %s", code, k.errb.String())
	}
	if k.out.String() != filepath.Join(k.dir, "o.jpg")+"\n" {
		t.Fatalf("stdout %q", k.out.String())
	}
	calls := k.run.Calls()
	tools := make([]string, 0, len(calls))
	for _, c := range calls {
		tools = append(tools, c.Name+" "+c.Args[0])
	}
	if !slices.Equal(tools, []string{"exiftool -config", "magick " + img, "exiftool -config"}) {
		t.Fatalf("calls %q", tools)
	}
	for _, want := range []string{"    1 image files updated\n", "watermark: compose visible watermark → ", "watermark: write metadata (EXIF/IPTC/XMP + XMP-wm sentinel)"} {
		if !strings.Contains(k.errb.String(), want) {
			t.Errorf("stderr lacks %q: %q", want, k.errb.String())
		}
	}
}

func TestM14AFailingToolHasItsCodeAndAFailedMetadataWriteRemovesTheOutput(t *testing.T) {
	k := newKit(t)
	k.semantic()
	img := k.image("p.jpg", jpegBytes)
	k.run.On([]string{"magick"}, execx.Result{ExitCode: 1})
	k.wantErr(k.exec(img), 4, "magick failed (visible watermark)")

	k = newKit(t)
	k.semantic()
	img = k.image("p.jpg", jpegBytes)
	k.run.On([]string{"exiftool", "-config", k.exifCfg, "-overwrite_original"}, execx.Result{ExitCode: 1})
	k.wantErr(k.exec(img), 5, "exiftool failed (metadata write)")
	if _, err := os.Stat(filepath.Join(k.dir, "pics", "p.watermarked.jpg")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the unmarked output is still there: %v", err)
	}
}

type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errors.New("closed") }

func TestB30AStdoutThatCannotBeWrittenIsExit1(t *testing.T) {
	k := newKit(t)
	k.semantic()
	k.stdout = brokenWriter{}
	if code := k.exec(k.image("p.jpg", jpegBytes)); code != 1 {
		t.Fatalf("exit %d", code)
	}
	if !strings.Contains(k.errb.String(), "watermark: could not write the output: closed") {
		t.Fatalf("stderr %q", k.errb.String())
	}
}
