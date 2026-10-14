//go:build !ccyteamstest

package ops

// trustPlays is false in every build but the parity suite's: a play runs under Ansible with the
// user's sudo, so the environment must not be able to point it outside the checkout, and no
// switch the environment can flip may relax that.
const trustPlays = false
