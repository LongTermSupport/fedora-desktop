package watermark

import (
	"fmt"
	"strings"
)

// options is one command line (M1). A semantic value the command line did not give is nil, so
// an empty one given on purpose still overrides the config files.
type options struct {
	text, artist, copyright, licenceURL, licenceSummary *string

	profile, configFile, output string

	force, dryRun, verbose, noTile, noCorner, showConfig, help bool

	images []string
}

// parseArgs reads the command line left to right. -h stops it (an error before it wins); --
// makes every later argument an image.
func parseArgs(args []string) (options, error) {
	var o options
	values := map[string]**string{
		"--text": &o.text, "--artist": &o.artist, "--copyright": &o.copyright,
		"--licence-url": &o.licenceURL, "--licence-summary": &o.licenceSummary,
	}
	paths := map[string]*string{"--profile": &o.profile, "--config": &o.configFile, "--output": &o.output}
	flags := map[string]*bool{
		"--force": &o.force, "--dry-run": &o.dryRun, "-v": &o.verbose, "--verbose": &o.verbose,
		"--no-tile": &o.noTile, "--no-corner": &o.noCorner, "--show-config": &o.showConfig,
	}
	rest := args
	for len(rest) > 0 {
		arg := rest[0]
		rest = rest[1:]
		if arg == "-o" {
			arg = "--output"
		}
		value, takesValue := values[arg]
		path, takesPath := paths[arg]
		switch {
		case arg == "-h" || arg == "--help":
			o.help = true
			return o, nil
		case arg == "--":
			o.images = append(o.images, rest...)
			return o, nil
		case takesValue || takesPath:
			if len(rest) == 0 {
				return o, fmt.Errorf("%s requires a value", arg)
			}
			given := rest[0]
			rest = rest[1:]
			if takesValue {
				*value = &given
			} else {
				*path = given
			}
		case flags[arg] != nil:
			*flags[arg] = true
		case strings.HasPrefix(arg, "-"):
			return o, fmt.Errorf("unknown option: %s (try --help)", arg)
		default:
			o.images = append(o.images, arg)
		}
	}
	return o, nil
}
