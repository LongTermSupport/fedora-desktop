// Package installplay holds the tests of the play that installs the v2 bus on a host
// (playbooks/imports/play-teamchat.yml, Plan 00246 INSTALL-PATH.md) and of the files it reads: it
// has no code of its own. Its tests hold the play's paths equal to the ones internal/teamchat/paths
// names, every architecture handled in every place that names one, and the play's tasks free of
// anything only a desktop's checkout and login user have, since the play runs against a remote
// inventory host. It is test-only on purpose: it reads YAML, which no program of the bus may
// link (the go-deps keep-out rows for teamchat and teamchat-admin).
package installplay
