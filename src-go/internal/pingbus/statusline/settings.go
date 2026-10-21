package statusline

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"syscall"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/pingbus/pyjson"
)

// settingsError is a settings file that exists but cannot be read or decoded (S8). Its text is
// shown in the status line, so the Python exception name in it is part of the output.
type settingsError struct{ msg string }

func (e *settingsError) Error() string { return e.msg }

const (
	blockingIOError = "BlockingIOError"
	brokenPipeError = "BrokenPipeError"
)

// osErrorNames are the OSError subclasses Python raises for an errno; any other is OSError (S8).
var osErrorNames = map[syscall.Errno]string{
	syscall.EAGAIN:       blockingIOError,
	syscall.EALREADY:     blockingIOError,
	syscall.EINPROGRESS:  blockingIOError,
	syscall.ECHILD:       "ChildProcessError",
	syscall.EPIPE:        brokenPipeError,
	syscall.ESHUTDOWN:    brokenPipeError,
	syscall.ECONNABORTED: "ConnectionAbortedError",
	syscall.ECONNREFUSED: "ConnectionRefusedError",
	syscall.ECONNRESET:   "ConnectionResetError",
	syscall.EEXIST:       "FileExistsError",
	syscall.ENOENT:       "FileNotFoundError",
	syscall.EINTR:        "InterruptedError",
	syscall.EISDIR:       "IsADirectoryError",
	syscall.ENOTDIR:      "NotADirectoryError",
	syscall.EACCES:       "PermissionError",
	syscall.EPERM:        "PermissionError",
	syscall.ESRCH:        "ProcessLookupError",
	syscall.ETIMEDOUT:    "TimeoutError",
}

// pythonExceptionName is the name of the Python exception the same failure raises, as the
// settings error prints it (S8).
func pythonExceptionName(err error) string {
	if errno, ok := errors.AsType[syscall.Errno](err); ok {
		if name, known := osErrorNames[errno]; known {
			return name
		}
	}
	return "OSError"
}

// settingsFiles are the files the inner command is looked for in, in precedence order (S7),
// spelt as pathlib prints them.
func settingsFiles(project string, hasProject bool, env map[string]string) []string {
	var files []string
	if hasProject {
		files = append(files,
			pyJoin(project, ".claude/settings.local.json"),
			pyJoin(project, ".claude/settings.json"))
	}
	user := env["CLAUDE_CONFIG_DIR"]
	if user == "" && strings.HasPrefix(env["HOME"], "/") {
		user = env["HOME"] + "/.claude"
	}
	if user != "" {
		files = append(files, pyJoin(user, "settings.json"))
	}
	return files
}

// readSettings is a settings file's bytes, read as Python's open does (following a link); a
// relative path is read from cwd.
func readSettings(path, cwd string) ([]byte, error) {
	if !strings.HasPrefix(path, "/") {
		path = pyJoin(cwd, path)
	}
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_CLOEXEC, 0)
	if err != nil {
		return nil, err
	}
	file := os.NewFile(uintptr(fd), path)
	raw, readErr := io.ReadAll(file)
	closeErr := file.Close()
	if readErr != nil {
		return nil, readErr
	}
	return raw, closeErr
}

// innerCommand is the status line command the session would run without the kit, or "" when
// there is none. This command itself is passed over (S7). A settings file that exists but
// cannot be read or decoded stops the search with a settingsError (S8).
func innerCommand(project string, hasProject bool, env map[string]string, cwd string) (string, error) {
	for _, path := range settingsFiles(project, hasProject, env) {
		raw, err := readSettings(path, cwd)
		if isErrno(err, syscall.ENOENT) {
			continue
		}
		if err != nil {
			return "", &settingsError{fmt.Sprintf("%s cannot be read (%s)", path, pythonExceptionName(err))}
		}
		data, err := pyjson.DecodeUTF8(raw, pyjson.Options{})
		if errors.Is(err, pyjson.ErrNotUTF8) {
			return "", &settingsError{path + " cannot be read (UnicodeDecodeError)"}
		}
		if err != nil {
			return "", &settingsError{path + " is not valid JSON"}
		}
		if command := commandOf(data); command != "" {
			return command, nil
		}
	}
	return "", nil
}

// commandOf is the usable command of a settings document's statusLine object: a string that is
// not blank and not this command itself. It is returned untrimmed.
func commandOf(data any) string {
	object, ok := data.(pyjson.Object)
	if !ok {
		return ""
	}
	line, ok := object["statusLine"].(pyjson.Object)
	if !ok {
		return ""
	}
	command, ok := line["command"].(string)
	if !ok {
		return ""
	}
	trimmed := strings.TrimSpace(command)
	if trimmed == "" || trimmed == commandName {
		return ""
	}
	return command
}

// commandName is this command's own text, which a settings file may name without looping (S7).
const commandName = "pingbus status-line"

// terminalColumns is COLUMNS when it is 1 to 6 decimal digits above zero, else 0 (S28).
func terminalColumns(env map[string]string) int {
	value := env["COLUMNS"]
	if value == "" || len(value) > 6 {
		return 0
	}
	for _, c := range value {
		if c < '0' || c > '9' {
			return 0
		}
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
