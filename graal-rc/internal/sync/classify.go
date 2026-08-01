package sync

// State is the classification of one script's sync status.
type State string

const (
	StateInSync            State = "in-sync"
	StateConvergent        State = "convergent"          // local==server but base stale/absent -> just update base
	StateLocalChanged      State = "local-changed"       // AUTO-PUSH
	StateServerChanged     State = "server-changed"      // AUTO-PULL
	StateConflict          State = "conflict"            // both changed -> review
	StateServerOnlyNew     State = "server-only-new"     // new on server -> auto-pull
	StateLocalMissingKeep  State = "local-missing-keep"  // local file gone, server present -> review (never delete server)
	StateNewLocal          State = "new-local"           // local-only, no baseline -> review (never auto-create)
	StateServerMissingKeep State = "server-missing-keep" // server gone, local present -> review (never delete local)
	StateBothDeleted       State = "both-deleted"        // drop manifest, no I/O
	StateInitialConflict   State = "initial-conflict"    // first-run, both present, differ -> review
)

// isReview reports whether a state requires human review (never auto-resolved).
func (s State) isReview() bool {
	switch s {
	case StateConflict, StateInitialConflict, StateNewLocal,
		StateLocalMissingKeep, StateServerMissingKeep:
		return true
	}
	return false
}

// classify decides the state from the three hashes. localHash/serverHash are ""
// when the side is absent; baseHash is "" when there is no baseline yet.
func classify(localHash, serverHash, baseHash string) State {
	localPresent := localHash != ""
	serverPresent := serverHash != ""

	switch {
	case !localPresent && !serverPresent:
		// Tracked but gone from both sides.
		if baseHash != "" {
			return StateBothDeleted
		}
		return StateInSync // not tracked anywhere; caller drops it

	case localPresent && serverPresent:
		if localHash == serverHash {
			if baseHash == localHash {
				return StateInSync
			}
			return StateConvergent // equalize base
		}
		// Both present, differ.
		if baseHash == "" {
			return StateInitialConflict // no baseline to attribute the change to
		}
		localChanged := localHash != baseHash
		serverChanged := serverHash != baseHash
		switch {
		case localChanged && !serverChanged:
			return StateLocalChanged
		case serverChanged && !localChanged:
			return StateServerChanged
		default: // both changed
			return StateConflict
		}

	case localPresent && !serverPresent:
		if baseHash == "" {
			return StateNewLocal
		}
		return StateServerMissingKeep

	default: // !localPresent && serverPresent
		if baseHash == "" {
			return StateServerOnlyNew
		}
		return StateLocalMissingKeep
	}
}
