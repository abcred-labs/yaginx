package management

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
)

// APITokenValidator authenticates a database-backed API credential. MCP is
// one consumer of this validator; future API consumers can reuse it.
type APITokenValidator func(context.Context, string) bool

type mcpRPC struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}
type mcpResponse struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id,omitempty"`
	Result  any    `json:"result,omitempty"`
	Error   any    `json:"error,omitempty"`
}

type diagnoseInput struct {
	RequestID  string `json:"requestId"`
	From       *int64 `json:"from"`
	To         *int64 `json:"to"`
	Host       string `json:"host"`
	Path       string `json:"path"`
	StatusCode *int   `json:"statusCode"`
	ErrorCode  string `json:"errorCode"`
}

// MCPStore is the request-history slice the diagnose tool reads. A store that
// also implements SystemErrorStore gets related system errors attached to the
// diagnosis; one that does not simply reports none.
type MCPStore interface {
	GetRequestHistory(context.Context, string) (RequestHistoryDetail, error)
	SearchRequestHistory(context.Context, RequestHistorySearchQuery) (RequestHistoryPage, error)
}

func MCPHandler(store MCPStore, validate APITokenValidator) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", "POST")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		const prefix = "Bearer "
		auth := r.Header.Get("Authorization")
		if validate == nil || len(auth) <= len(prefix) || !strings.EqualFold(auth[:len(prefix)], prefix) || !validate(r.Context(), strings.TrimSpace(auth[len(prefix):])) {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		var call mcpRPC
		if json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&call) != nil {
			writeMCP(w, call.ID, nil, map[string]any{"code": -32700, "message": "invalid JSON"})
			return
		}
		if call.JSONRPC != "2.0" {
			writeMCP(w, call.ID, nil, map[string]any{"code": -32600, "message": "invalid request"})
			return
		}
		switch call.Method {
		case "initialize":
			writeMCP(w, call.ID, map[string]any{"protocolVersion": "2025-03-26", "capabilities": map[string]any{"tools": map[string]any{}}, "serverInfo": map[string]string{"name": "yaginx", "version": "1"}}, nil)
		case "tools/list":
			writeMCP(w, call.ID, map[string]any{"tools": []any{map[string]any{"name": "diagnose_request", "description": "Diagnose a stored Yaginx proxy request and related system errors.", "inputSchema": map[string]any{"type": "object"}}}}, nil)
		case "tools/call":
			handleDiagnoseMCP(w, call.ID, call.Params, store)
		default:
			writeMCP(w, call.ID, nil, map[string]any{"code": -32601, "message": "method not found"})
		}
	})
}

func handleDiagnoseMCP(w http.ResponseWriter, id any, raw json.RawMessage, store MCPStore) {
	var p struct {
		Name      string        `json:"name"`
		Arguments diagnoseInput `json:"arguments"`
	}
	if json.Unmarshal(raw, &p) != nil || p.Name != "diagnose_request" {
		writeMCP(w, id, nil, map[string]any{"code": -32602, "message": "unknown tool"})
		return
	}
	in := p.Arguments
	var detail RequestHistoryDetail
	var candidates RequestHistoryPage
	var err error
	if strings.TrimSpace(in.RequestID) != "" {
		detail, err = store.GetRequestHistory(contextBackground(), strings.TrimSpace(in.RequestID))
		if err != nil {
			writeMCP(w, id, nil, map[string]any{"code": -32004, "message": "request not found"})
			return
		}
	} else {
		q := RequestHistorySearchQuery{PageRequest: PageRequest{PageIndex: 1, PageSize: 20}, Host: in.Host, Path: in.Path, StatusCode: in.StatusCode, ErrorCode: in.ErrorCode}
		if in.From != nil {
			v := Int64Input(*in.From)
			q.From = &v
		}
		if in.To != nil {
			v := Int64Input(*in.To)
			q.To = &v
		}
		candidates, err = store.SearchRequestHistory(contextBackground(), q)
		if err != nil {
			writeMCP(w, id, nil, map[string]any{"code": -32000, "message": "request history unavailable"})
			return
		}
		if len(candidates.Records) == 1 {
			detail, err = store.GetRequestHistory(contextBackground(), candidates.Records[0].ID)
		}
	}
	result := map[string]any{"request": detail, "matchedCandidates": candidates.Records, "relatedSystemErrors": []SystemErrorListItem{}, "diagnosis": diagnosisSummary(detail)}
	if err == nil && detail.TraceID != "" {
		if s, ok := store.(SystemErrorStore); ok {
			if page, e := s.SearchSystemErrorsByTrace(contextBackground(), detail.TraceID, 20); e == nil {
				result["relatedSystemErrors"] = page.Records
			}
		}
	}
	writeMCP(w, id, result, nil)
}

func diagnosisSummary(d RequestHistoryDetail) string {
	if d.ErrorCode != "" {
		return "proxy error code: " + d.ErrorCode
	}
	if d.ErrorCategory != "" {
		return "proxy error category: " + d.ErrorCategory
	}
	if d.StatusCode >= 500 {
		return "server error response"
	}
	if d.StatusCode >= 400 {
		return "client error response"
	}
	return "no classified proxy error"
}
func writeMCP(w http.ResponseWriter, id, result, problem any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(mcpResponse{JSONRPC: "2.0", ID: id, Result: result, Error: problem})
}
func contextBackground() context.Context { return context.Background() }
