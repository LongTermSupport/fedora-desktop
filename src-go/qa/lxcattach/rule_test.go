package lxcattach

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// lines is the finding lines of one script judged alone.
func lines(t *testing.T, body string) []int {
	t.Helper()
	found, err := Analyze([]Source{{Path: "a.bash", Text: []byte("#!/usr/bin/env bash\n" + body)}})
	if err != nil {
		t.Fatal(err)
	}
	got := make([]int, 0, len(found))
	for _, f := range found {
		got = append(got, f.Line)
	}
	return slices.Compact(got)
}

func TestTheRuleJudgesEachShape(t *testing.T) {
	for _, c := range []struct {
		name, body string
		want       []int
	}{
		{"stderr to a file", "sudo -n lxc-attach -n c -- true 2>\"$err\"\n", []int{2}},
		{"stdout to a file", "sudo lxc-attach -n c -- true >out\n", []int{2}},
		{"stdout appended", "lxc-attach -n c -- true >>out\n", []int{2}},
		{"both streams", "lxc-attach -n c -- true &>out\n", []int{2}},
		{"both streams appended", "lxc-attach -n c -- true &>>out\n", []int{2}},
		{"clobber", "lxc-attach -n c -- true >|out\n", []int{2}},
		{"csh-style both", "lxc-attach -n c -- true >&out\n", []int{2}},
		{"stdin from a file", "lxc-attach -n c -- cat <in\n", []int{2}},
		{"stdin read-write", "lxc-attach -n c -- cat <>in\n", []int{2}},
		{"a path to the tool", "/usr/bin/lxc-attach -n c -- true 2>e\n", []int{2}},
		{"a fixture path to the tool", "\"${FIX}/lxc-attach\" -n c -- true 2>e\n", []int{2}},
		{"null is skipped by lxc-attach", "lxc-attach -n c -- true </dev/null >/dev/null 2>/dev/null\n", []int{}},
		{"null for both", "lxc-attach -n c -- true &>/dev/null\n", []int{}},
		{"another device", "lxc-attach -n c -- true </dev/zero\n", []int{}},
		{"a pipe each way", "printf x | lxc-attach -n c -- cat 2>&1 | cat\n", []int{}},
		{"stderr to a process substitution", "lxc-attach -n c -- true 2> >(cat >\"$err\")\n", []int{}},
		{"a capture", "out=\"$(lxc-attach -n c -- true)\"\n", []int{}},
		{"a capture whose stderr is a file", "out=\"$(lxc-attach -n c -- true 2>e)\"\n", []int{2}},
		{"a capture of a group sent to a file", "{ out=\"$(lxc-attach -n c -- true)\"; } 2>e\n", []int{2}},
		{"a capture of a group with stdout to a file", "{ out=\"$(lxc-attach -n c -- true)\"; } >e\n", []int{}},
		{"stderr dup to a file stdout", "lxc-attach -n c -- true >o 2>&1\n", []int{2}},
		{"stderr dup to a piped stdout", "lxc-attach -n c -- true 2>&1 | cat >o\n", []int{}},
		{"pipe-all", "lxc-attach -n c -- true |& cat >o\n", []int{}},
		{"stdout dup to stderr on a file", "lxc-attach -n c -- true 2>e >&2\n", []int{2}},
		{"closed", "lxc-attach -n c -- true 2>&-\n", []int{}},
		{"stdin piped past a file", "printf x | lxc-attach -n c -- cat 2>&1 | cat; cat <in\n", []int{}},
		{"a heredoc", "lxc-attach -n c -- cat <<EOF\nx\nEOF\n", []int{}},
		{"a here-string", "lxc-attach -n c -- cat <<<x\n", []int{}},
		{"an enclosing group", "{\n  lxc-attach -n c -- true\n} 2>e\n", []int{3}},
		{"an enclosing subshell", "(\n  lxc-attach -n c -- true\n) >o\n", []int{3}},
		{"an enclosing loop", "while read -r c; do\n  lxc-attach -n \"$c\" -- true\ndone <list\n", []int{3}},
		{"an enclosing loop whose call takes null", "while read -r c; do\n  lxc-attach -n \"$c\" -- true </dev/null\ndone <list\n", []int{}},
		{"an if", "if lxc-attach -n c -- true; then :; fi 2>e\n", []int{2}},
		{"and-or", "true && lxc-attach -n c -- true 2>e || false\n", []int{2}},
		{"a function body under a redirect at its call", "f() { lxc-attach -n c -- \"$@\"; }\nf true 2>e\n", []int{3}},
		{"a function body redirected itself", "f() {\n  lxc-attach -n c -- true\n} 2>e\nf\n", []int{3}},
		{"a wrapper of a wrapper", "g() { lxc-attach -n c -- \"$@\"; }\nf() { g \"$@\"; }\nf true 2>e\n", []int{4}},
		{"a wrapper called by a runner", "f() { lxc-attach -n c -- \"$@\"; }\ntimeout 5 f true >o\n", []int{3}},
		{"an array variable", "A=(sudo -n lxc-attach)\n\"${A[@]}\" -n c -- true 2>e\n", []int{3}},
		{"a scalar variable", "A=lxc-attach\n$A -n c -- true 2>e\n", []int{3}},
		{"a declared array", "f() { local -a A=(sudo lxc-attach); \"${A[@]}\" -n c -- true; }\nf 2>e\n", []int{3}},
		{"the tool named to a program that does not run it", "assert_contains what \"$hint\" lxc-attach >o\ngrep -c lxc-attach notes >o\n", []int{}},
		{"the tool after a runner, by path", "best_effort_rm what /usr/bin/sudo lxc-attach -n c -- true 2>e\n", []int{2}},
		{"a wrapper after a runner", "f() { lxc-attach -n c -- \"$@\"; }\nexec f 2>e\n", []int{3}},
		{"a variable built from a variable", "A=(sudo lxc-attach)\nB=(\"${A[@]}\" -n c)\n\"${B[@]}\" -- true 2>e\n", []int{4}},
		{"an unrelated function", "f() { echo lxc-attach is the tool; }\nf 2>e\n", []int{}},
		{"an unrelated variable", "A=(sudo podman)\n\"${A[@]}\" exec c true 2>e\n", []int{}},
		{"a word that is not the tool", "printf 'lxc-attach -n c' >o\n", []int{}},
		{"fd 3 carried into a group", "{ lxc-attach -n c -- true 2>&3; } 3>e\n", []int{2}},
		{"fd 3 carried from a pipe", "{ lxc-attach -n c -- true 1>&3 | cat; } 3>&1 | cat\n", []int{}},
		{"stderr through /dev/stderr onto a file", "{ lxc-attach -n c -- true >/dev/stderr; } 2>e\n", []int{2}},
		{"stdout through /dev/fd/1 onto a pipe", "lxc-attach -n c -- true 2>/dev/fd/1 | cat\n", []int{}},
		{"a process substitution reading the tool", "while read -r l; do :; done < <(lxc-attach -n c -- true 2>e)\n", []int{2}},
		{"a process substitution writing to the tool", "tee >(lxc-attach -n c -- cat >o) </dev/null\n", []int{2}},
		{"a for loop, a case and a coproc", "for c in a; do case $c in a) lxc-attach -n c -- true ;; esac; done >o\n", []int{2}},
		{"time", "time lxc-attach -n c -- true 2>e\n", []int{2}},
		{"a background job", "lxc-attach -n c -- true >o &\n", []int{2}},
		{"negation", "! lxc-attach -n c -- true 2>e\n", []int{2}},
		{"in a test", "[[ -n \"$(lxc-attach -n c -- true 2>e)\" ]]\n", []int{2}},
		{"in an arithmetic", "(( $(lxc-attach -n c -- true 2>e) ))\n", []int{2}},
		{"declared with a substitution", "local x=\"$(lxc-attach -n c -- true 2>e)\"\n", []int{2}},
		{"a wrapper that pipes every stream", "f() {\n  { ( cat | lxc-attach -n c -- \"$@\" 3>&- | cat 3>&- ) 2>&1 1>&3 3>&- | cat >&2 3>&-; } 3>&1\n}\nf true <i >o 2>e\n", []int{}},
		{"a wrapper that pipes stderr only", "f() { lxc-attach -n c -- \"$@\" 2>&1 | cat; }\nf true 2>e\nf true <i\n", []int{4}},
		{"a wrapper that joins stderr to stdout", "f() { lxc-attach -n c -- \"$@\" 2>&1; }\nf true 2>e\nf true >o\n", []int{4}},
		{"a wrapper handed a descriptor above 2", "f() { lxc-attach -n c -- \"$@\" 2>&3; }\nf true 3>o\n", []int{3}},
		{"a wrapper with a nested function", "f() {\n  g() { lxc-attach -n c -- true; }\n  true\n}\nf 2>e\ng 2>e\n", []int{7}},
		{"a wrapper defined twice", "f() { lxc-attach -n c -- true </dev/null; }\nf() { lxc-attach -n c -- true 2>/dev/null; }\nf <i\n", []int{4}},
		{"a fd above 2", "lxc-attach -n c -- true 3>o\n", []int{}},
		{"a named fd", "lxc-attach -n c -- true {fd}>o\n", []int{}},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := lines(t, c.body); !slices.Equal(got, c.want) {
				t.Errorf("lines %v, want %v\n%s", got, c.want, c.body)
			}
		})
	}
}

// A wrapper defined in one script is a wrapper in every script judged with it, which is how a plan's
// acceptance sources its stage files.
func TestAWrapperCountsAcrossTheScriptsJudgedTogether(t *testing.T) {
	found, err := Analyze([]Source{
		{Path: "lib.bash", Text: []byte("LXC=(sudo -n lxc-attach)\nin_guest() { \"${LXC[@]}\" -n \"$1\" -- \"${@:2}\"; }\n")},
		{Path: "use.bash", Text: []byte("in_guest c true 2>\"$err\"\n")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(found) != 1 || found[0].Path != "use.bash" || found[0].Line != 1 || found[0].Stream != "stderr" || found[0].RedirLine != 1 {
		t.Fatalf("%+v", found)
	}
	if got := found[0].String(); got != "use.bash:1: lxc-attach-file-stream: stderr is a file (redirected on line 1), and lxc-attach makes it root's" {
		t.Errorf("%q", got)
	}
}

func TestEachStreamIsNamedOnce(t *testing.T) {
	found, err := Analyze([]Source{{Path: "a.bash", Text: []byte("{\nlxc-attach -n c -- cat <i >o 2>e\n} 2>x\n")}})
	if err != nil {
		t.Fatal(err)
	}
	streams := make([]string, 0, len(found))
	for _, f := range found {
		streams = append(streams, f.Stream)
	}
	if strings.Join(streams, ",") != "stdin,stdout,stderr" {
		t.Fatalf("%+v", found)
	}
}

func TestAScriptThatDoesNotParseIsAnError(t *testing.T) {
	if _, err := Analyze([]Source{{Path: "bad.bash", Text: []byte("if then fi (\n")}}); err == nil || !strings.Contains(err.Error(), "bad.bash") {
		t.Fatalf("%v", err)
	}
}

// The fixture shipped with the gate marks exactly the lines the rule reports.
func TestTheFixtureMarksWhatTheRuleReports(t *testing.T) {
	text, err := os.ReadFile(filepath.Join("..", "..", "..", FixturePath))
	if err != nil {
		t.Fatal(err)
	}
	marks := FixtureMarks(string(text))
	got, err := FixtureFindings(string(text))
	if err != nil {
		t.Fatal(err)
	}
	if len(marks) < 10 || !slices.Equal(marks, got) {
		t.Fatalf("marks %v, rule %v", marks, got)
	}
}
