package admin

import (
	"fmt"

	"github.com/LongTermSupport/fedora-desktop/src-go/internal/teamchat/paths"
)

// Class is who may run a verb (C 3.3).
type Class int

// The caller classes; ClassAny is `version`'s, which every class may run.
const (
	ClassRoot Class = iota + 1
	ClassCtl
	ClassOther
	ClassAny
)

func (c Class) String() string {
	switch c {
	case ClassRoot:
		return "root"
	case ClassCtl:
		return paths.CtlUser
	case ClassOther:
		return "a user other than root and " + paths.CtlUser
	case ClassAny:
		return "any user"
	}
	return fmt.Sprintf("class %d", int(c))
}

// classOf is the caller's class from its real and effective uid only (CA2): both must name
// the class, so a mixed pair is anyone else.
func classOf(p Process) (Class, error) {
	ctl, found, err := p.LookupUser(paths.CtlUser)
	if err != nil {
		return 0, fmt.Errorf("cannot look up the %s account: %w", paths.CtlUser, err)
	}
	uid, euid := p.Getuid(), p.Geteuid()
	switch {
	case uid != euid:
		return ClassOther, nil
	case uid == 0:
		return ClassRoot, nil
	case found && uid == ctl.UID:
		return ClassCtl, nil
	}
	return ClassOther, nil
}
