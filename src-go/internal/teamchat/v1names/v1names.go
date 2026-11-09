// Package v1names holds the v1 names v2 holds on purpose, so that nothing else in v2 names one
// (Plan 00209, DESIGN-v2-D-migration.md section 4.1). The gate teamchat-keeps-off-v1
// (src-go/qa/teamchatseparate) scans every other v2 file for them and leaves this package out of
// its scope. A unit that needs a v1 name takes it from here and never writes it.
package v1names

// InaccessiblePaths is the unit line every v2 system unit carries (D 4.2, C 3.2.3): the six
// places v1 keeps its state, its install records, its applied stamps, its configuration, its
// binaries and its shared files. The - on each lets a unit start on a host that has no v1, and
// a path v2 code reaches by mistake then fails with EACCES instead of touching v1.
const InaccessiblePaths = "InaccessiblePaths=" +
	"-/var/lib/agent-bus " +
	"-/var/lib/agent-bus-install " +
	"-/var/lib/agent-bus-applied " +
	"-/etc/agent-bus " +
	"-/usr/local/lib/agent-bus " +
	"-/usr/local/share/agent-bus"
