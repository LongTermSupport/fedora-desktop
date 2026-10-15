package watermark

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/shellword"
)

// The image signatures M8 accepts.
var (
	jpegMagic = []byte{0xFF, 0xD8, 0xFF}
	pngMagic  = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}
)

// extensions are the names M8 accepts, lower-cased.
var extensions = []string{".jpg", ".jpeg", ".png"}

// checkImage is M8: a regular file with a JPEG or PNG name and JPEG or PNG content (B27: the
// content is read here, not by file(1)).
func checkImage(path string) error {
	info, err := os.Stat(path)
	if err != nil || !info.Mode().IsRegular() {
		return fail(exitUsage, "no such file: %s", path)
	}
	ext := strings.ToLower(filepath.Ext(path))
	known := false
	for _, e := range extensions {
		known = known || ext == e
	}
	if !known {
		return fail(exitUsage, "unsupported extension (JPEG and PNG only): %s", path)
	}
	f, err := os.Open(path)
	if err != nil {
		return fail(exitUsage, "cannot read %s: %v", path, err)
	}
	head := make([]byte, len(pngMagic))
	n, err := io.ReadFull(f, head)
	if cerr := f.Close(); cerr != nil {
		return fail(exitUsage, "cannot read %s: %v", path, cerr)
	}
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return fail(exitUsage, "cannot read %s: %v", path, err)
	}
	head = head[:n]
	if !bytes.HasPrefix(head, jpegMagic) && !bytes.HasPrefix(head, pngMagic) {
		return fail(exitUsage, "input content is not a JPEG or PNG image: %s", path)
	}
	return nil
}

// hasMarkedName is M9's name probe.
func hasMarkedName(path string) bool {
	lower := strings.ToLower(path)
	for _, e := range extensions {
		if strings.HasSuffix(lower, ".watermarked"+e) {
			return true
		}
	}
	return false
}

// defaultOutput is M10's <dir>/<stem>.watermarked.<ext>, the extension lower-cased.
func defaultOutput(input string) string {
	ext := filepath.Ext(input)
	return strings.TrimSuffix(input, ext) + ".watermarked" + strings.ToLower(ext)
}

// resolve is the path with every symlink in its existing part followed, its missing tail kept
// as given, as realpath -m makes it (M10).
func resolve(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("absolute path of %s: %w", path, err)
	}
	tail := ""
	cur := abs
	for {
		resolved, err := filepath.EvalSymlinks(cur)
		if err == nil {
			return filepath.Join(resolved, tail), nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", fmt.Errorf("resolve %s: %w", cur, err)
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return abs, nil
		}
		tail = filepath.Join(filepath.Base(cur), tail)
		cur = parent
	}
}

// magickArgv is M11.
func magickArgv(s Settings, o options, input, output string) []string {
	argv := []string{magickBin, input}
	if !o.noTile {
		argv = append(argv,
			"(",
			"-size", "400x400", "xc:none",
			"-font", s.Font,
			"-pointsize", "%[fx:w*"+s.SizePctTile+"]",
			"-fill", "rgba(255,255,255,"+s.OpacityTile+")",
			"-stroke", "rgba(0,0,0,"+s.OpacityTile+")", "-strokewidth", "1",
			"-gravity", "center", "-annotate", s.TileAngle, s.Text,
			"-write", "mpr:tile", "+delete",
			")",
			"(",
			"+clone", "-fill", "mpr:tile", "-draw", "color 0,0 reset",
			")",
			"-compose", "over", "-composite",
		)
	}
	if !o.noCorner {
		argv = append(argv,
			"-font", s.Font,
			"-pointsize", "%[fx:w*"+s.SizePctCorner+"]",
			"-fill", "rgba(255,255,255,"+s.OpacityCorner+")",
			"-stroke", "rgba(0,0,0,"+s.OpacityCorner+")", "-strokewidth", "2",
			"-gravity", "southeast", "-annotate", "+30+30", s.Text,
		)
	}
	return append(argv, "-quality", s.Quality, output)
}

// exiftoolArgv is M12.
func exiftoolArgv(s Settings, config, output, appliedAt string) []string {
	return []string{
		exiftoolBin, "-config", config, "-overwrite_original",
		"-EXIF:Artist=" + s.Artist,
		"-EXIF:Copyright=" + s.Copyright,
		"-IPTC:By-line=" + s.Artist,
		"-IPTC:CopyrightNotice=" + s.Copyright,
		"-XMP-dc:Creator=" + s.Artist,
		"-XMP-dc:Rights=" + s.Copyright,
		"-XMP-xmpRights:Marked=True",
		"-XMP-xmpRights:UsageTerms=" + s.LicenceSummary,
		"-XMP-xmpRights:WebStatement=" + s.LicenceURL,
		"-XMP-wm:Applied=True",
		"-XMP-wm:AppliedAt=" + appliedAt,
		"-XMP-wm:AppliedBy=" + toolID,
		output,
	}
}

// joinArgv is a command shown by --dry-run (M13, B28).
func joinArgv(argv []string) string { return shellword.Join(argv) }

// showConfig is M4's text.
func showConfig(s Settings, o options) string {
	q := shellword.Quote
	flag := func(b bool) string {
		if b {
			return "1"
		}
		return "0"
	}
	lines := []string{
		"# resolved config (post-precedence-chain)",
		"artist=" + q(s.Artist),
		"copyright=" + q(s.Copyright),
		"licence_url=" + q(s.LicenceURL),
		"licence_summary=" + q(s.LicenceSummary),
		"text=" + q(s.Text),
		"font=" + q(s.Font),
		"opacity_corner=" + q(s.OpacityCorner),
		"opacity_tile=" + q(s.OpacityTile),
		"tile_angle=" + q(s.TileAngle),
		"size_pct_corner=" + q(s.SizePctCorner),
		"size_pct_tile=" + q(s.SizePctTile),
		"quality=" + q(s.Quality),
		"output=" + q(o.output),
		"profile=" + q(o.profile),
		"config_file=" + q(o.configFile),
		"no_tile=" + flag(o.noTile),
		"no_corner=" + flag(o.noCorner),
	}
	return strings.Join(lines, "\n") + "\n"
}
