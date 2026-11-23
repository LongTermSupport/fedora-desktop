package state

// Floor stands for a room's floor: the millisecond and the event IDs at it already taken.
type Floor struct {
	MS  int64
	IDs []string
}

// Covers judges an event inside Floor's own method, the one place its time is compared.
func (f Floor) Covers(ts int64, id string) bool {
	if ts < f.MS {
		return true
	}
	for _, x := range f.IDs {
		if ts == f.MS && x == id {
			return true
		}
	}
	return false
}

// Max is the later floor, inside a method too.
func (f Floor) Max(o Floor) Floor {
	if o.MS > f.MS {
		return o
	}
	return f
}
