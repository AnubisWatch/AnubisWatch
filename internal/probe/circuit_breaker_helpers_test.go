package probe

import "time"

// isOpen is the breaker's legacy skip predicate. The engine no longer skips
// checks while a breaker is open (that hid downtime), so only the breaker's
// state-machine tests use it.
func (cb *circuitBreaker) isOpen(cfg CircuitBreakerConfig) bool {
	if !cfg.Enabled {
		return false
	}

	cb.mu.RLock()
	state := cb.state
	lastChange := cb.lastStateChange
	cb.mu.RUnlock()

	if state == "closed" {
		return false
	}

	if state == "open" {
		// Check if timeout has elapsed to transition to half-open
		if time.Since(lastChange) >= cfg.Timeout {
			// Try to transition to half-open
			cb.mu.Lock()
			// Double-check state after acquiring lock
			if cb.state == "open" {
				cb.state = "half-open"
				cb.successes = 0
				cb.lastStateChange = time.Now()
			}
			cb.mu.Unlock()
			return false
		}
		return true
	}

	// half-open: allow checks but monitor results
	return false
}
