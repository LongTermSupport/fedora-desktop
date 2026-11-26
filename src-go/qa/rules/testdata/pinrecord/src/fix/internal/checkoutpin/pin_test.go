package checkoutpin

// Plant records unasked: a test sets up the store.
func Plant(s Store) error { return s.Record("x") }
