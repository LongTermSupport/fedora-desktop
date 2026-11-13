// Package elsewhere has a file of the exempt file's name outside teamchat/paths: judged.
package elsewhere

// Dir names the directory where the exemption does not reach.
func Dir(runtime string) string {
	return runtime + "/cc-socks" // want `Claude Code's inbox socket directory`
}
