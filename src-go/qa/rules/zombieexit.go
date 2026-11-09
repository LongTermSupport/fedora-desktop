package rules

import (
	"go/ast"
	"go/token"
	"strconv"
	"strings"

	"golang.org/x/tools/go/analysis"
)

// ZombieExit forbids judging a process ended by the zombie state its /proc stat shows.
var ZombieExit = &analysis.Analyzer{
	Name: "project_zombie_exit",
	Doc: `project_zombie_exit flags a string or character literal whose text is Z (" Z", "Z", 'Z')
in a function (its function literals included) that also holds a string literal naming a /proc
stat file ("stat", or one that ends in "/stat") and none that starts with "Threads" (the thread
count of /proc/<pid>/status, which makes the reading sound).

/proc/<pid>/stat is the thread-group leader's own entry, and its state letter turns Z as soon
as the leader thread has exited. A multi-threaded program (every Go program) is killed thread by
thread: the leader can read Z while the others are still exiting, and the files the process
holds (a flock, a socket, a pipe end) are let go only when the last of them drops the file
table. A test that waits for Z and then reads anything that depends on the release races those
threads: TestClaudeStatusLine read the status line once the killed link showed Z and found
link.lock still held (issue #182). An experiment with a child holding a flock and running 8 or
32 busy threads found the lock still held at the first Z reading in 65 and 155 of 300 rounds.

Not flagged: a function that reads no stat path, and one that also reads the thread count. Not
caught: a stat path built entirely at run time, a state compared through a parameter (procIs(pid,
'Z') in a helper that takes the letter), and a Threads literal that is read but not required.

How to fix: read /proc/<pid>/status and require both "State:" Z (or X) and "Threads:" 1: the
other threads are counted until each is released, which is after it dropped the file table, so
the pair holds only once the whole group has ended. Better still, wait for the effect the test
asserts (the lock free, the line drawn) with a bounded wait, rather than for the process.`,
	Run:        perPass(runZombieExit),
	ResultType: noResultType,
}

func runZombieExit(pass *analysis.Pass) {
	for _, f := range pass.Files {
		for _, decl := range f.Decls {
			fd, ok := decl.(*ast.FuncDecl)
			if !ok || fd.Body == nil || !readsProcStat(fd.Body) || countsThreads(fd.Body) {
				continue
			}
			ast.Inspect(fd.Body, func(n ast.Node) bool {
				lit, isLit := n.(*ast.BasicLit)
				if isLit && isZombieLetter(lit) {
					pass.Reportf(lit.Pos(), "%s is the zombie state of the thread-group leader, which its other threads "+
						"outlive while they still hold its files: require State Z and Threads 1 in /proc/<pid>/status, "+
						"or wait for the effect itself", lit.Value)
				}
				return true
			})
		}
	}
}

// readsProcStat is whether body holds a string literal naming a /proc stat file.
func readsProcStat(body ast.Node) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if s, ok := stringLit(n); ok && (s == "stat" || strings.HasSuffix(s, "/stat")) {
			found = true
		}
		return !found
	})
	return found
}

// countsThreads is whether body holds a string literal naming the thread count of
// /proc/<pid>/status.
func countsThreads(body ast.Node) bool {
	found := false
	ast.Inspect(body, func(n ast.Node) bool {
		if s, ok := stringLit(n); ok && strings.HasPrefix(s, "Threads") {
			found = true
		}
		return !found
	})
	return found
}

// isZombieLetter is whether lit is a string or character literal whose text, spaces trimmed,
// is Z.
func isZombieLetter(lit *ast.BasicLit) bool {
	if lit.Kind != token.STRING && lit.Kind != token.CHAR {
		return false
	}
	if lit.Kind == token.CHAR {
		return lit.Value == "'Z'"
	}
	s, err := strconv.Unquote(lit.Value)
	return err == nil && strings.TrimSpace(s) == "Z"
}

// stringLit is the value of n when it is a string literal.
func stringLit(n ast.Node) (string, bool) {
	lit, ok := n.(*ast.BasicLit)
	if !ok || lit.Kind != token.STRING {
		return "", false
	}
	s, err := strconv.Unquote(lit.Value)
	return s, err == nil
}
