package management

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

type Envelope struct {
	Code   int             `json:"code"`
	Msg    *string         `json:"msg"`
	ErrMsg *string         `json:"errMsg"`
	TID    string          `json:"tid"`
	Data   json.RawMessage `json:"data"`
}

func TransactionID() string {
	b := make([]byte, 12)
	if _, err := rand.Read(b); err != nil {
		return "unavailable"
	}
	return hex.EncodeToString(b)
}

func write[T any](w http.ResponseWriter, status, code int, data T, errMsg string) {
	writeWithTransactionID(w, status, code, TransactionID(), data, errMsg)
}

func writeWithTransactionID[T any](w http.ResponseWriter, status, code int, tid string, data T, errMsg string) {
	raw, err := json.Marshal(data)
	if err != nil {
		raw = []byte("null")
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Act-Trace-Id", tid)
	w.WriteHeader(status)
	var errorMessage *string
	if errMsg != "" {
		errorMessage = &errMsg
	}
	_ = json.NewEncoder(w).Encode(Envelope{Code: code, TID: tid, Data: raw, ErrMsg: errorMessage})
}

// OK writes the success envelope used by every management operation.
func OK[T any](w http.ResponseWriter, data T) { write(w, http.StatusOK, http.StatusOK, data, "") }

// Fail writes the error envelope, downgrading the legacy 500 that the
// handwritten stack returned for malformed requests to a 400.
func Fail(w http.ResponseWriter, status int, message string) {
	if status == http.StatusInternalServerError && message == "invalid request" {
		status = http.StatusBadRequest
	}
	tid := TransactionID()
	if reporter, ok := w.(*journalResponseWriter); ok && reporter.logger != nil && message != "" {
		reporter.logger.Error("management request failed", "traceId", tid, "method", reporter.method, "path", reporter.path, "error", message)
	}
	FailWithTransactionID(w, status, tid, message)
}

func FailWithTransactionID(w http.ResponseWriter, status int, tid, message string) {
	msg := "调用异常"
	if status == http.StatusUnauthorized {
		msg = "授权认证失败"
	} else if status == http.StatusForbidden {
		msg = "无对应授权"
	} else if status == http.StatusServiceUnavailable {
		msg = "服务暂时不可用"
	}
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Act-Trace-Id", tid)
	w.WriteHeader(status)
	data, err := json.Marshal(map[string]string{"requestId": tid})
	if err != nil {
		data = []byte("null")
	}
	_ = json.NewEncoder(w).Encode(Envelope{Code: status, Msg: &msg, TID: tid, Data: data, ErrMsg: &message})
}

// WriteUnavailable is used by the LKG-only management listener. It preserves
// the normal envelope while rejecting every stale management operation.
func WriteUnavailable(w http.ResponseWriter) {
	Fail(w, http.StatusServiceUnavailable, "数据库当前不可写")
}
