package statusline

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf8"
)

// settingsError is a settings file that exists but cannot be read or decoded (S8). Its text is
// shown in the status line, so the Python exception name in it is part of the output.
type settingsError struct{ msg string }

func (e *settingsError) Error() string { return e.msg }

// decodeJSON is the JSON reader the status line uses for the payload, the settings and the
// membership: a duplicate key takes its last value, as Python's json does.
func decodeJSON(raw []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var data any
	if err := dec.Decode(&data); err != nil {
		return nil, err
	}
	if dec.More() {
		return nil, errors.New("trailing data after the JSON value")
	}
	return data, nil
}

// pythonExceptionName is the name of the Python exception the same failure raises, as the
// settings error prints it (S8).
func pythonExceptionName(err error) string {
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		return "OSError"
	}
	switch errno {
	case syscall.ENOENT:
		return "FileNotFoundError"
	case syscall.EISDIR:
		return "IsADirectoryError"
	case syscall.EACCES, syscall.EPERM:
		return "PermissionError"
	case syscall.ENOTDIR:
		return "NotADirectoryError"
	}
	return "OSError"
}

// settingsFiles are the files the inner command is looked for in, in precedence order (S7).
func settingsFiles(project string, hasProject bool, env map[string]string) []string {
	var files []string
	if hasProject {
		files = append(files,
			filepath.Join(project, ".claude", "settings.local.json"),
			filepath.Join(project, ".claude", "settings.json"))
	}
	user := env["CLAUDE_CONFIG_DIR"]
	if user == "" && strings.HasPrefix(env["HOME"], "/") {
		user = env["HOME"] + "/.claude"
	}
	if user != "" {
		files = append(files, filepath.Join(user, "settings.json"))
	}
	return files
}

// innerCommand is the status line command the session would run without the kit, or "" when
// there is none. This command itself is passed over (S7). A settings file that exists but
// cannot be read or decoded stops the search with a settingsError (S8).
func innerCommand(project string, hasProject bool, env map[string]string) (string, error) {
	for _, path := range settingsFiles(project, hasProject, env) {
		raw, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return "", &settingsError{fmt.Sprintf("%s cannot be read (%s)", path, pythonExceptionName(err))}
		}
		if !utf8.Valid(raw) {
			return "", &settingsError{fmt.Sprintf("%s cannot be read (UnicodeDecodeError)", path)}
		}
		data, err := decodeJSON(raw)
		if err != nil {
			return "", &settingsError{fmt.Sprintf("%s is not valid JSON", path)}
		}
		if line, ok := statusLineOf(data); ok {
			if command := commandOf(line); command != "" {
				return command, nil
			}
		}
	}
	return "", nil
}

// statusLineOf is the statusLine object of a settings document, if it has one.
func statusLineOf(data any) (map[string]any, bool) {
	object, ok := data.(map[string]any)
	if !ok {
		return nil, false
	}
	line, ok := object["statusLine"].(map[string]any)
	return line, ok
}

// commandOf is the usable command of a statusLine object: a string that is not blank and not
// this command itself. It is returned untrimmed.
func commandOf(line map[string]any) string {
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
