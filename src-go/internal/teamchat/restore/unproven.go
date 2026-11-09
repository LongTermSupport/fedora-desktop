package restore

// ErrUnproven is the production wiring's answer for the restore engine until host checks H-C5
// and H-C15 pass (DC8, n2g-D8, RW2): a refusal, so the verb exits 70 before it takes the lock.
var ErrUnproven error = &RefusedError{Problem: "restore is not enabled: host checks H-C5 and H-C15 have not passed (DC8)"}
