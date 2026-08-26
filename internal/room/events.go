package room

// Subscribe registers a room change listener and returns its cancellation function.
func (s *Service) Subscribe(
	code string,
) (events <-chan struct{}, unsubscribe func()) {
	ch := make(chan struct{}, 1)

	s.mu.Lock()

	if s.events[code] == nil {
		s.events[code] = make(map[chan struct{}]struct{})
	}

	s.events[code][ch] = struct{}{}

	s.mu.Unlock()

	return ch, func() {
		s.mu.Lock()

		delete(s.events[code], ch)

		if len(s.events[code]) == 0 {
			delete(s.events, code)
		}

		s.mu.Unlock()
	}
}

// Notify signals all listeners that a room has changed.
func (s *Service) Notify(code string) {
	s.mu.Lock()
	defer s.mu.Unlock()

	for ch := range s.events[code] {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
