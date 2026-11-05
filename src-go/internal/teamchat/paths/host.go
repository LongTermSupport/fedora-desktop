package paths

// The names `team apply` and `team remove` add to the host (C 3.2.2-3.2.4, 3.5.2, 3.5.3).

// systemdDir is where the play installs the homeserver's base unit and `team apply` its drop-in.
const systemdDir = "/etc/systemd/system"

// ServerUnitTemplateFile is the installed base unit of every team's homeserver.
const ServerUnitTemplateFile = systemdDir + "/" + ServerUnitTemplate

// UnitsRecord is the team's restart and reload digest sets, with the Tuwunel version, port and
// listen set last started.
func UnitsRecord(t TeamID) string { return AdminState(t) + "/units.json" }

// ServerNameFile is the team's server_name, fixed before the homeserver's first start.
func ServerNameFile(t TeamID) string { return AdminState(t) + "/server-name" }

// ServerDropInFile is the drop-in `team apply` renders for the team's homeserver unit.
func ServerDropInFile(t TeamID) string { return systemdDir + "/" + ServerUnitDropIn(t) + "/team.conf" }

// HomeserverDatabase is the homeserver's database, as the unit sees it.
func HomeserverDatabase(t TeamID) string { return HomeserverState(t) + "/db" }

// HomeserverBackups is Tuwunel's own backup engine output, as the unit sees it.
func HomeserverBackups(t TeamID) string { return HomeserverState(t) + "/backups" }

// HomeserverPrivateDatabase is the homeserver's database where systemd keeps it, as root sees it
// whether or not the unit runs.
func HomeserverPrivateDatabase(t TeamID) string { return HomeserverPrivateState(t) + "/db" }
