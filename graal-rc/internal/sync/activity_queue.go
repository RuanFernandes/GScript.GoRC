package sync

import (
	"context"
	"log"
	"time"
)

const maxPendingActivities = 512

// EnqueueChatLine never waits for network I/O. Repeated changes to a resource
// replace its queued state; overflow requests a full snapshot instead of
// retaining an unbounded backlog or silently missing a server deletion.
func (e *Engine) EnqueueChatLine(line string) {
	activity, ok := ParseChatLine(line)
	if !ok {
		return
	}
	key := activity.Kind + ":" + activity.Name
	e.mu.Lock()
	defer e.mu.Unlock()
	if !e.running {
		return
	}
	if _, exists := e.activityPending[key]; exists {
		e.activityPending[key] = activity
	} else if len(e.activityPending) < maxPendingActivities {
		e.activityPending[key] = activity
		e.activityOrder = append(e.activityOrder, key)
	} else {
		e.activityOverflow = true
	}
	select {
	case e.activityWake <- struct{}{}:
	default:
	}
}

func (e *Engine) activityLoop(ctx context.Context, stop <-chan struct{}, wake <-chan struct{}, done chan<- struct{}) {
	defer close(done)
	for {
		select {
		case <-ctx.Done():
			return
		case <-stop:
			return
		case <-wake:
		}
		for {
			e.workMu.Lock()
			e.mu.Lock()
			if !e.running || e.activityWake != wake || ctx.Err() != nil {
				e.mu.Unlock()
				e.workMu.Unlock()
				return
			}
			if len(e.activityOrder) > 0 && (e.panicMode || (e.cfg.PauseUntil != 0 && e.now().Unix() < e.cfg.PauseUntil)) {
				e.activityOverflow = true
			}
			if e.activityOverflow {
				generation := e.syncGeneration
				e.activityOverflow = false
				e.activityPending = make(map[string]Activity)
				e.activityOrder = nil
				e.mu.Unlock()
				log.Printf("sync activity queue reached %d resources; reconciling a complete snapshot", maxPendingActivities)
				e.reconcileLocked(ctx, false)
				e.mu.Lock()
				incomplete := e.syncGeneration == generation
				if incomplete {
					e.activityOverflow = true
				}
				e.mu.Unlock()
				e.workMu.Unlock()
				if incomplete {
					timer := time.NewTimer(5 * time.Second)
					select {
					case <-ctx.Done():
						timer.Stop()
						return
					case <-stop:
						timer.Stop()
						return
					case <-timer.C:
					}
				}
				continue
			}
			if len(e.activityOrder) == 0 {
				e.mu.Unlock()
				e.workMu.Unlock()
				break
			}
			key := e.activityOrder[0]
			e.activityOrder[0] = ""
			e.activityOrder = e.activityOrder[1:]
			activity := e.activityPending[key]
			delete(e.activityPending, key)
			e.mu.Unlock()
			if activity.Action == "deleted" {
				e.handleDeleteActivity(activity)
			} else {
				e.handleActivity(activity)
			}
			e.workMu.Unlock()
		}
	}
}

func (e *Engine) failSnapshot(err error) {
	e.mu.Lock()
	e.lastError = err.Error()
	e.progress.Active = false
	e.mu.Unlock()
	e.emitStatus()
}
