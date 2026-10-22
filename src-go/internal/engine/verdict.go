package engine

import "strings"

// Posture is whether an engine is rootless, by its own report.
type Posture int

// The three answers. PostureUnknown is not a pass: an engine whose posture cannot be read is
// not an engine known to be safe.
const (
	PostureUnknown Posture = iota
	PostureRootless
	PostureRootful
)

// unknownWord is how an unknown verdict prints.
const unknownWord = "unknown"

// The two words podman's boolean report fields print.
const (
	reportTrue  = "true"
	reportFalse = "false"
)

func (p Posture) String() string {
	switch p {
	case PostureRootless:
		return "rootless"
	case PostureRootful:
		return "rootful"
	case PostureUnknown:
	}
	return unknownWord
}

// squeeze removes every whitespace character, so a trailing newline or padding is not a third
// state of a one-word answer.
func squeeze(s string) string { return strings.Join(strings.Fields(s), "") }

// RootlessVerdict reads an engine's report of its own posture: podman's
// `info --format {{.Host.Security.Rootless}}` (exactly true or false) or docker's
// `info --format {{.SecurityOptions}}` (a list that names rootless only on a rootless daemon).
// Anything else, an engine it does not know included, is PostureUnknown.
func RootlessVerdict(kind Kind, report string) Posture {
	report = squeeze(report)
	if report == "" {
		return PostureUnknown
	}
	switch kind {
	case Podman:
		switch report {
		case reportTrue:
			return PostureRootless
		case reportFalse:
			return PostureRootful
		}
	case Docker:
		switch {
		case strings.Contains(report, "name=rootless"):
			return PostureRootless
		case strings.Contains(report, "name="):
			return PostureRootful
		}
	}
	return PostureUnknown
}

// SELinux is whether this host's SELinux will refuse a container's reads of a home-directory
// bind, and so whether the binds carry a relabel.
type SELinux int

// The four answers. SELinuxUnknown relabels: relabelling a readable tree costs nothing, while
// not relabelling an unreadable one costs the session.
const (
	SELinuxUnknown SELinux = iota
	SELinuxEnforcing
	SELinuxPermissive
	SELinuxOff
)

func (s SELinux) String() string {
	switch s {
	case SELinuxEnforcing:
		return "enforcing"
	case SELinuxPermissive:
		return "permissive"
	case SELinuxOff:
		return "off"
	case SELinuxUnknown:
	}
	return unknownWord
}

// Relabel is the mount option that makes a bind readable under confinement: the shared "z",
// so a second session on the same project can still read it, unless SELinux is off.
func (s SELinux) Relabel() string {
	if s == SELinuxOff {
		return ""
	}
	return "z"
}

// SELinuxVerdict reads getenforce's answer ("" when it is not installed) and the engine's
// report of whether it labels ("true" or "false"). Permissive relabels too: nothing is
// refused, but every unrelabelled read is logged. getenforce's own spelling is required.
func SELinuxVerdict(enforce, report string) SELinux {
	enforce, report = squeeze(enforce), squeeze(report)
	labels := func(yes SELinux) SELinux {
		switch report {
		case reportTrue:
			return yes
		case reportFalse:
			return SELinuxOff
		default:
			return SELinuxUnknown
		}
	}
	switch enforce {
	case "Disabled":
		return SELinuxOff
	case "Enforcing":
		return labels(SELinuxEnforcing)
	case "Permissive":
		return labels(SELinuxPermissive)
	case "":
		// No SELinux userland: only the engine's word is left, and only its "no" is final.
		if report == reportFalse {
			return SELinuxOff
		}
		return SELinuxUnknown
	default:
		return SELinuxUnknown
	}
}

// DockerSELinuxReport is docker's SecurityOptions list reduced to podman's two words: "true"
// when the list names selinux, "false" when it is a list that does not. Text that is not such
// a list (an unreachable daemon's error) is returned as it is, which reads as unknown.
func DockerSELinuxReport(options string) string {
	switch {
	case strings.Contains(options, "selinux"):
		return reportTrue
	case strings.Contains(options, "name="):
		return reportFalse
	default:
		return options
	}
}
