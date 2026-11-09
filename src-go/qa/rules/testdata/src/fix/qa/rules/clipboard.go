package rules

// The rules package names the self-erasing flags it looks for.
const pasteOnce = "--paste-once"

// It names the liveness words it looks for too.
const livenessClaim = "online"

func claim() string { return pasteOnce + ": the password is on the clipboard for one paste" }
