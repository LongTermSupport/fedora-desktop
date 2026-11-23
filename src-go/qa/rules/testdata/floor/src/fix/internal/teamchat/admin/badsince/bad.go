// Package badsince holds a wall-clock since bound cut so that its own millisecond is lost.
package badsince

type timed struct{ ts int64 }

// older is the export's read back to since, as it was: an event at since is dropped.
func older(e timed, since int64) bool {
	return e.ts <= since // want `a since bound cut by <= or >`
}

// newer is the same cut written the other way round.
func newer(e timed, sinceMS int64) bool {
	return sinceMS > e.ts || e.ts > sinceMS // want `a since bound cut by <= or >` `a since bound cut by <= or >`
}

// kept is the boundary kept: only an event before since is dropped.
func kept(e timed, since int64) bool {
	return e.ts < since || since <= 0 && e.ts >= since
}

// position is a since that is a stream position, the right form of a cutoff: not judged.
func position(pos, since int) bool { return pos > since }

// aged is an age threshold outside the member tree, named a floor: not an event cut.
func aged(atMS, floor int64) bool { return atMS < floor }
