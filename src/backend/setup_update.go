package backend

// Update applies a read-modify-write update under the store lock.
// It preserves unknown keys, which is important when the setup UI only edits a subset
// of settings while the backend also stores derived auth/session fields.
func (s *SetupStore) Update(fn func(map[string]any) map[string]any) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	current, err := s.loadUnlocked()
	if err != nil {
		return err
	}
	updated := fn(current)
	if updated == nil {
		updated = map[string]any{}
	}
	return s.saveUnlocked(updated)
}
