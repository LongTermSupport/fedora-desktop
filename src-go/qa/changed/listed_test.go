package changed

import (
	"strings"
	"testing"
)

func TestReadListedReadsPathsAndRefusesAMalformedLine(t *testing.T) {
	scripts := t.TempDir()
	writeFile(t, scripts, "list.txt", "# comment\n\na.bash\t1\t2\nb.bash\t3\n")
	s := &session{scripts: scripts, listed: map[string]bool{}}
	if err := s.readListed("list.txt"); err != nil {
		t.Fatal(err)
	}
	if !s.listed["a.bash"] || !s.listed["b.bash"] || len(s.listed) != 2 {
		t.Fatalf("listed %v", s.listed)
	}
	for name, body := range map[string]string{
		"no tab":     "a.bash\t1\nstray words\n",
		"empty path": "a.bash\t1\n\t5\n",
	} {
		dir := t.TempDir()
		writeFile(t, dir, "list.txt", body)
		bad := &session{scripts: dir, listed: map[string]bool{}}
		err := bad.readListed("list.txt")
		if err == nil || !strings.Contains(err.Error(), "list.txt:2") {
			t.Errorf("%s: got %v", name, err)
		}
	}
}
