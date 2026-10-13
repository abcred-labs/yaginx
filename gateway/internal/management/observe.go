package management

import "time"

// ObserveRequest records a management operation's latency and outcome.
type ObserveRequest func(operation, result string, duration time.Duration)

// Observe labels the operation success or failure and reports its latency. It
// tolerates a nil hook so a domain handler never has to guard the call.
func (observe ObserveRequest) Observe(operation string, err error, started time.Time) {
	if observe == nil {
		return
	}
	result := "success"
	if err != nil {
		result = "failure"
	}
	observe(operation, result, time.Since(started))
}
