// Package placeissued is `teamchat seat place-issued` (Plan 00246 Task 8.8, X21, X22): an issued
// seat, read from stdin, placed in this machine's seat pool for one checkout, so that a seat
// issued on the host (teamchat-admin member add, return or reissue) reaches the machine whose
// sessions will take it. Nothing here starts a process or opens a connection to the host that
// issued the seat: the document arrives on stdin, by whatever pipe the owner builds.
//
// Read judges the document (strict JSON within a cap; the issued document itself or the answer
// of a member verb around it; then seat.DecodeIssued). Place reads, opens the seat store of the
// checkout, takes its allocation lock, moves the leftovers of a crashed placement to quarantine
// and claims, fills and swaps the seat in by the store's own placement (seat.Claim, seat.Fill):
// a seat with no record is created, a seat with one is replaced and its old record quarantined
// with its credential unlinked. It never takes or releases a seat. A refusal is a FailureError
// that Report writes as the member binary writes errors, its object always the last stderr line.
package placeissued
