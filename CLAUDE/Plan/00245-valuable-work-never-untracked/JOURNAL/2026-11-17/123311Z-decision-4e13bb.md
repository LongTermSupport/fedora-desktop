## 12:33 · decision · #310 — Q23: a first word is never a term alone

Owner decision (question Q23): #310 takes option 1. A name's first word is never indexed as a private term on its own, and a stored list kept from the older rule drops such a first word unless it is freshly learned as a whole name. Implemented test-first on the private-names-310 branch (the TEAM and member parsing, and withoutFirstWords for the stored list). The same branch adds the denylist mode that go-qa-ci's public-safety scan reads.
