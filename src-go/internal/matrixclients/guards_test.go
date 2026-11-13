package matrixclients

// The rules that keep every guard against a human's Matrix session on the one list (Defence
// Before Fix, issue #251). Each failure names its rule, documented in CLAUDE/QA.md:
//
//   - matrix-client-unlisted: a Matrix client a play installs is not on the list.
//   - matrix-client-guard-uncovered: a guard that cannot read the list at run time (cc's deny
//     rules, the kit launcher) or the list's Go copy does not cover exactly what the list says.
//   - matrix-client-named-by-hand: a listed client's app id or native path is spelt out in code
//     that is neither the list, a covered guard, nor a file declared client-specific.
//
// They read the repository itself, from this package's directory.

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

const (
	repoRoot        = "../../.."
	canonicalList   = "helpers/pingbus/matrix_clients.json"
	listCopy        = "src-go/internal/matrixclients/clients.json"
	matrixPlay      = "playbooks/imports/optional/common/play-matrix-clients.yml"
	denySettings    = "files/var/local/claude-code/cc-teams-settings.json"
	kitLauncher     = "files/opt/claude-yolo/optional/agent-bus/agent-bus-claude"
	clientSpecific  = "src-go/internal/matrixclients/testdata/client-specific.tsv"
	ruleUnlisted    = "matrix-client-unlisted"
	ruleUncovered   = "matrix-client-guard-uncovered"
	ruleNamedByHand = "matrix-client-named-by-hand"
	// dot spells the Secret Service's bus name, and below a deny rule's "everything under" glob,
	// so no string here reads to the repository's guards as a secrets path or a comment.
	dot   = "."
	below = "/" + "**"
)

// keyringDenyRules are cc's denies on the Secret Service, required while any listed client keeps
// its session there.
var keyringDenyRules = []string{
	"Read(~/.local/share/keyrings" + below + ")", "Edit(~/.local/share/keyrings" + below + ")",
	"Bash(*secret-tool*)", "Bash(*keyring*)", "Bash(*freedesktop.Secret*)", "Bash(*freedesktop" + dot + "secrets*)",
}

// matrixWords mark an install anywhere as a Matrix client, by the names of the Matrix desktop
// clients packaged for Fedora or Flathub.
var matrixWords = regexp.MustCompile(`(?i)(riot|element|nheko|matrix|fractal|neochat|cinny|fluffychat|schildi|quaternion|gomuks|iamb|kazv|mirage|spectral|syphon)`)

func readRepo(t *testing.T, rel string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot, filepath.FromSlash(rel)))
	if err != nil {
		t.Fatalf("%s: %v", rel, err)
	}
	return data
}

func repoList(t *testing.T) List {
	t.Helper()
	list, err := Parse(readRepo(t, canonicalList))
	if err != nil {
		t.Fatalf("%s: %s: %v; see CLAUDE/QA.md %q", ruleUncovered, canonicalList, err, ruleUncovered)
	}
	return list
}

// The Go guards read the embedded copy, which is the canonical list byte for byte.
func TestTheEmbeddedCopyIsTheList(t *testing.T) {
	if !bytes.Equal(listJSON, readRepo(t, canonicalList)) {
		t.Fatalf("%s: %s differs from %s; copy the list over it. See CLAUDE/QA.md %q",
			ruleUncovered, listCopy, canonicalList, ruleUncovered)
	}
}

// install is one thing a YAML file installs: a Flatpak app id, a package name, or an app-id
// shaped value anywhere (a loop item).
type install struct {
	file, id, how string
	line          int
}

// yamlScan is what one YAML file installs, and the task files it imports.
type yamlScan struct {
	file     string
	installs []install
	imports  []string
}

// pair notes what one key and its value install or import.
func (s *yamlScan) pair(key string, value *yaml.Node) {
	switch key {
	case "flatpak_app_id":
		if value.Kind == yaml.ScalarNode {
			s.installs = append(s.installs, install{s.file, value.Value, "flatpak_app_id", value.Line})
		}
	case "community.general.flatpak", "flatpak", "ansible.builtin.dnf", "dnf", "ansible.builtin.package", "package":
		for _, name := range moduleNames(value) {
			s.installs = append(s.installs, install{s.file, name.Value, key, name.Line})
		}
	case "ansible.builtin.import_tasks", "import_tasks", "ansible.builtin.include_tasks", "include_tasks":
		if value.Kind == yaml.ScalarNode {
			s.imports = append(s.imports, strings.TrimPrefix(value.Value, "{{ root_dir }}/"))
		}
	case "loop", "with_items", "with_list":
		for _, item := range value.Content {
			if item.Kind == yaml.ScalarNode && appIDPattern.MatchString(item.Value) {
				s.installs = append(s.installs, install{s.file, item.Value, "loop item", item.Line})
			}
		}
	}
}

// scanYAML is what one YAML file installs, and the task files it imports.
func scanYAML(file string, data []byte) ([]install, []string, error) {
	s := &yamlScan{file: file}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	for {
		var doc yaml.Node
		if err := dec.Decode(&doc); errors.Is(err, io.EOF) {
			return s.installs, s.imports, nil
		} else if err != nil {
			return nil, nil, fmt.Errorf("%s: %w", file, err)
		}
		walkYAML(&doc, s.pair)
	}
}

// moduleNames are the name entries of a package module's arguments.
func moduleNames(args *yaml.Node) []*yaml.Node {
	if args.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(args.Content); i += 2 {
		if args.Content[i].Value != "name" {
			continue
		}
		value := args.Content[i+1]
		if value.Kind == yaml.ScalarNode {
			return []*yaml.Node{value}
		}
		return slices.DeleteFunc(slices.Clone(value.Content), func(n *yaml.Node) bool { return n.Kind != yaml.ScalarNode })
	}
	return nil
}

func walkYAML(n *yaml.Node, pair func(string, *yaml.Node)) {
	if n.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(n.Content); i += 2 {
			pair(n.Content[i].Value, n.Content[i+1])
			walkYAML(n.Content[i+1], pair)
		}
		return
	}
	for _, child := range n.Content {
		walkYAML(child, pair)
	}
}

// isAnsible is whether a repository path is YAML under playbooks/ or tasks/.
func isAnsible(p string) bool {
	ext := path.Ext(p)
	return (strings.HasPrefix(p, "playbooks/") || strings.HasPrefix(p, "tasks/")) && (ext == ".yml" || ext == ".yaml")
}

// scanAnsible is what every play and task file installs, and the files each imports.
func scanAnsible(root fs.FS) (map[string][]install, map[string][]string, error) {
	scanned := map[string][]install{}
	importsOf := map[string][]string{}
	err := fs.WalkDir(root, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || !d.Type().IsRegular() || !isAnsible(p) {
			return err
		}
		data, err := fs.ReadFile(root, p)
		if err != nil {
			return err
		}
		installs, imports, err := scanYAML(p, data)
		scanned[p], importsOf[p] = installs, imports
		return err
	})
	return scanned, importsOf, err
}

// matrixFiles are the Matrix clients play and every file it imports, however deep.
func matrixFiles(importsOf map[string][]string) map[string]bool {
	files := map[string]bool{}
	for queue := []string{matrixPlay}; len(queue) > 0; queue = queue[1:] {
		if !files[queue[0]] {
			files[queue[0]] = true
			queue = append(queue, importsOf[queue[0]]...)
		}
	}
	return files
}

// matrixInstalls are the Matrix clients the repository's plays install: every Flatpak the Matrix
// clients play and the task files it imports install, and anything installed anywhere under
// playbooks/ or tasks/ whose name is a Matrix client's.
func matrixInstalls(root fs.FS) ([]install, error) {
	scanned, importsOf, err := scanAnsible(root)
	if err != nil {
		return nil, err
	}
	if _, ok := scanned[matrixPlay]; !ok {
		return nil, fmt.Errorf("%s is missing: the Matrix clients play is where this rule starts", matrixPlay)
	}
	inPlay := matrixFiles(importsOf)
	files := make([]string, 0, len(scanned))
	for file := range scanned {
		files = append(files, file)
	}
	slices.Sort(files)
	var found []install
	for _, file := range files {
		for _, in := range scanned[file] {
			byPlay := inPlay[file] && in.how == "flatpak_app_id"
			if byPlay || matrixWords.MatchString(in.id) {
				found = append(found, in)
			}
		}
	}
	return found, nil
}

// unlisted are the installs whose id is no listed client's app id, each id once per file.
func unlisted(list List, installs []install) []install {
	var out []install
	for _, in := range installs {
		listed := slices.ContainsFunc(list.Clients, func(c Client) bool { return c.AppID == in.id })
		seen := slices.ContainsFunc(out, func(o install) bool { return o.file == in.file && o.id == in.id })
		if !listed && !seen {
			out = append(out, in)
		}
	}
	return out
}

// matrix-client-unlisted, on the repository: every Matrix client a play installs is listed.
func TestEveryMatrixClientInstalledIsListed(t *testing.T) {
	list := repoList(t)
	installs, err := matrixInstalls(os.DirFS(repoRoot))
	if err != nil {
		t.Fatal(err)
	}
	if !slices.ContainsFunc(installs, func(in install) bool { return in.id == "im.riot.Riot" }) {
		t.Fatalf("the scan found no Element install, so it cannot be trusted: %+v", installs)
	}
	for _, in := range unlisted(list, installs) {
		t.Errorf("%s: %s:%d installs %s (%s), a Matrix client %s does not list; add it with its data paths. "+
			"See CLAUDE/QA.md %q", ruleUnlisted, in.file, in.line, in.id, in.how, canonicalList, ruleUnlisted)
	}
}

// matrix-client-unlisted, on its fixture: the Matrix play's Flatpaks, its imported task file's,
// and a Matrix client installed by any other play are found; an unrelated Flatpak is not.
func TestTheUnlistedRuleFindsItsFixture(t *testing.T) {
	installs, err := matrixInstalls(os.DirFS("testdata/unlisted"))
	if err != nil {
		t.Fatal(err)
	}
	found := unlisted(List{Clients: []Client{{AppID: "im.riot.Riot"}}}, installs)
	got := make([]string, 0, len(found))
	for _, in := range found {
		got = append(got, in.file+" "+in.id)
	}
	want := []string{
		"playbooks/imports/optional/common/play-matrix-clients.yml im.nheko.Nheko",
		"playbooks/imports/optional/common/play-matrix-clients.yml org.lightning_matrix.Lightning",
		"playbooks/imports/play-other.yml nheko",
		"playbooks/imports/play-other.yml org.gnome.Fractal",
		"tasks/client.yml org.example.SomeClient",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("found %q\nwant %q", got, want)
	}
}

// denyRules are the cc deny rules the list requires.
func denyRules(list List) []string {
	var rules []string
	keyring := false
	for _, c := range list.Clients {
		// Claude Code's home-relative rule form is a literal tilde, not an abbreviated home.
		flatpak := c.FlatpakDir() + below
		rules = append(rules, "Read(~/"+flatpak+")", "Edit(~/"+flatpak+")", "Bash(*"+c.AppID+"*)")
		for _, n := range c.Native {
			dir := n.Parent + "/" + n.Name
			rules = append(rules, "Read(~/"+dir+below+")", "Edit(~/"+dir+below+")", "Bash(*"+dir+"*)")
			if n.Profiles {
				rules = append(rules, "Read(~/"+dir+"-*"+below+")", "Edit(~/"+dir+"-*"+below+")")
			}
		}
		keyring = keyring || c.Keyring
	}
	if keyring {
		rules = append(rules, keyringDenyRules...)
	}
	return rules
}

// matrix-client-guard-uncovered: cc's deny rules name every listed client's folders and app id.
func TestTheDenyRulesCoverEveryClient(t *testing.T) {
	var settings struct {
		Permissions struct {
			Deny []string `json:"deny"`
		} `json:"permissions"`
	}
	if err := json.Unmarshal(readRepo(t, denySettings), &settings); err != nil {
		t.Fatal(err)
	}
	for _, rule := range denyRules(repoList(t)) {
		if !slices.Contains(settings.Permissions.Deny, rule) {
			t.Errorf("%s: %s does not deny %s; add it. See CLAUDE/QA.md %q", ruleUncovered, denySettings, rule, ruleUncovered)
		}
	}
}

// launcherArray is the words of `readonly NAME=(...)` on one line of the kit launcher.
func launcherArray(text, name string) ([]string, bool) {
	m := regexp.MustCompile(`(?m)^readonly ` + name + `=\(([^)]*)\)$`).FindStringSubmatch(text)
	if m == nil {
		return nil, false
	}
	return strings.Fields(m[1]), true
}

// matrix-client-guard-uncovered: the kit launcher, which runs before pingbus and cannot read
// JSON, looks for exactly the list's profile paths and prefixes.
func TestTheKitLauncherCoversEveryClient(t *testing.T) {
	list := repoList(t)
	text := string(readRepo(t, kitLauncher))
	for _, array := range []struct {
		name string
		want []string
	}{{"MATRIX_PROFILE_PATHS", list.ProfilePaths()}, {"MATRIX_PROFILE_PREFIXES", list.ProfilePrefixes()}} {
		got, ok := launcherArray(text, array.name)
		if !ok || !slices.Equal(got, array.want) {
			t.Errorf("%s: %s: readonly %s=(%s) must be the list's, (%s). See CLAUDE/QA.md %q",
				ruleUncovered, kitLauncher, array.name, strings.Join(got, " "), strings.Join(array.want, " "), ruleUncovered)
		}
	}
}

// codeRoots are the trees the named-by-hand rule reads; test code, fixtures and vendored code are
// not guards.
var codeRoots = []string{"helpers", "src-go", "files", "scripts", "tasks", "playbooks", "run.bash"}

func notCode(p string, d fs.DirEntry) bool {
	base := path.Base(p)
	if d.IsDir() {
		return base == "vendor" || base == "testdata" || base == "__pycache__" || base == "node_modules"
	}
	return strings.HasSuffix(base, "_test.go") || strings.HasPrefix(p, "scripts/test-") || strings.HasSuffix(base, ".pyc")
}

// readClientSpecific is the files declared to configure a client itself (not to guard against
// one), each with its reason.
func readClientSpecific(t *testing.T) map[string]bool {
	t.Helper()
	files := map[string]bool{}
	sc := bufio.NewScanner(bytes.NewReader(readRepo(t, clientSpecific)))
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		file, reason, ok := strings.Cut(line, "\t")
		if !ok || strings.TrimSpace(reason) == "" {
			t.Fatalf("%s:%d: want <path><TAB><reason>", clientSpecific, n)
		}
		if _, err := os.Stat(filepath.Join(repoRoot, file)); err != nil {
			t.Errorf("%s:%d: %s: %v; remove the row", clientSpecific, n, file, err)
		}
		files[file] = true
	}
	return files
}

// namedByHand is each needle's first occurrence in the code under root, outside allowed, as
// "path:line needle".
func namedByHand(root fs.FS, needles []string, allowed map[string]bool) ([]string, error) {
	var hits []string
	for _, top := range codeRoots {
		err := fs.WalkDir(root, top, func(p string, d fs.DirEntry, err error) error {
			switch {
			case err != nil:
				return err
			case notCode(p, d) && d.IsDir():
				return fs.SkipDir
			case notCode(p, d) || allowed[p]:
				return nil
			}
			if !d.Type().IsRegular() {
				return nil
			}
			data, err := fs.ReadFile(root, p)
			for _, needle := range needles {
				if before, _, found := bytes.Cut(data, []byte(needle)); found {
					hits = append(hits, fmt.Sprintf("%s:%d names %s", p, bytes.Count(before, []byte("\n"))+1, needle))
				}
			}
			return err
		})
		if err != nil {
			return nil, err
		}
	}
	return hits, nil
}

// matrix-client-named-by-hand: no code outside the list, its covered consumers and the declared
// client-specific files spells out a listed client's app id or native path.
func TestNoGuardNamesAClientByHand(t *testing.T) {
	list := repoList(t)
	var needles []string
	for _, c := range list.Clients {
		needles = append(needles, c.AppID)
		for _, n := range c.Native {
			needles = append(needles, n.Parent+"/"+n.Name)
		}
	}
	allowed := readClientSpecific(t)
	for _, f := range []string{canonicalList, listCopy, denySettings, kitLauncher, clientSpecific} {
		allowed[f] = true
	}
	hits, err := namedByHand(os.DirFS(repoRoot), needles, allowed)
	if err != nil {
		t.Fatal(err)
	}
	for _, hit := range hits {
		t.Errorf("%s: %s by hand; read the list (%s) instead, or declare the file in %s if it configures "+
			"that client itself. See CLAUDE/QA.md %q", ruleNamedByHand, hit, canonicalList, clientSpecific, ruleNamedByHand)
	}
}
