// Package event defines data-plane request facts without telemetry dependencies.
package event

import "time"

const (
	ResponseModeNonStream = "non_stream"
	ResponseModeStream    = "stream"
	ResponseModeUpgrade   = "upgrade"
)

// Event contains request facts collected by the full gateway data plane.
type Event struct {
	Host, Domain, Method, Path, Query, ClientIP, Scheme, Protocol, TraceID string
	HistoryMode                                                            string
	HistorySuccessSamplingRate                                             *float64
	Status, HistoryStatus                                                  int
	StatusSource, ErrorCategory                                            string
	Inbound, Outbound, DurationMS                                          int64
	At                                                                     time.Time
	UserAgent                                                              string
	RequestHeaders, ResponseHeaders                                        map[string][]string
	RequestBodyBase64, ResponseBodyBase64                                  string
	RequestContentType, ResponseContentType                                string
	RequestBodyTruncated, ResponseBodyTruncated                            bool
	ExceptionType, ExceptionMessage, ExceptionStackTrace                   string
	ExceptionStage, ForwarderError, ProxyStage                             string
	UpstreamStatus                                                         *int
	BlacklistRuleID                                                        int64
	ResponseMode                                                           string
	TTFBMS, TTFTMS                                                         int64
}

// HistoryDecision is made once after the final proxy status is known. Keeping
// persistence and metadata capture in the same decision prevents sampling
// from allocating data that will never be queued.
type HistoryDecision struct {
	Record          bool
	CaptureMetadata bool
}

// AggregationEvent is the bounded request fact set used by traffic
// aggregation. It deliberately excludes request identity and payload data;
// those belong to Event and are retained only by request history policy.
type AggregationEvent struct {
	Host, Domain                  string
	At                            time.Time
	Status                        int
	StatusSource, ErrorCategory   string
	DurationMS, Inbound, Outbound int64
	UserAgent                     string
	ResponseMode                  string
	TTFBMS, TTFTMS                int64
}
