// Package status stands for teamchat's status record reader.
package status

// Read reads the record at path.
func Read(path, team string) (string, bool, error) { return path + team, false, nil }
