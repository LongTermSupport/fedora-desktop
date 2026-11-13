package badunit

// expected is a test's expected unit text: a test is not a unit the play installs.
const expected = "[Unit]\nDescription=a daemon\n[Service]\nRestart=always\n"

var _ = expected
