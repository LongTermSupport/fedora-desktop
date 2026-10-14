//go:build ccyteamstest

package teams

// trustPlays is true only in the binary the ccy-teams parity suite builds with
// -tags ccyteamstest, whose stub plays live outside the checkout. The production build never
// compiles this file.
const trustPlays = true
