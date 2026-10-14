package freeze

// List is the report of what is frozen and what is running, on stdout. "Frozen" is the frozen
// state; everything else counts as running, since an inventory holds only those two states.
func (s *Session) List() error {
	s.list()
	return s.withWrites(nil)
}

func (s *Session) list() {
	var running, frozen []string
	for _, e := range s.inv.Entries() {
		if e.State == s.Cfg.StateFrozen {
			frozen = append(frozen, e.Name)
		} else {
			running = append(running, e.Name)
		}
	}
	s.stdout.Printf("=== frozen (%d) ===\n", len(frozen))
	if len(frozen) == 0 {
		s.stdout.Printf("  (none)\n")
	} else {
		s.stdout.Printf("%s\n  Thaw them all with: %s thaw --all\n", s.Table(frozen...), s.Cfg.Tool)
	}
	s.stdout.Printf("\n=== running (%d) ===\n", len(running))
	if len(running) == 0 {
		s.stdout.Printf("  (none)\n")
	} else {
		s.stdout.Printf("%s", s.Table(running...))
	}
	if f, ok := s.Engine.(listFooterer); ok {
		s.stdout.Printf("%s", f.ListFooter(s.inv))
	}
}
