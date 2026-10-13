package proxy

import (
	"bytes"
	"compress/flate"
	"compress/gzip"
	"compress/zlib"
	"encoding/base64"
	"io"
	"mime"
	"net/http"
	"strings"
)

const (
	responseBodyMaxBytes           = 4095
	compressedResponseBodyMaxBytes = 512
)

// Response capture is independent of the request-body configuration. It never
// retains a prefix of an oversized body, or changes the bytes being forwarded.
type responseBodyCapture struct {
	encoding  string
	limit     int
	value     []byte
	truncated bool
}

func newResponseBodyCapture(encoding string, length int64) *responseBodyCapture {
	limit := responseBodyMaxBytes
	if encoding != "" && encoding != "identity" {
		limit = compressedResponseBodyMaxBytes
	}
	return &responseBodyCapture{encoding: encoding, limit: limit, truncated: length > int64(limit)}
}

func (c *responseBodyCapture) Append(value []byte) {
	if c.truncated || len(value) == 0 {
		return
	}
	if len(value) > c.limit-len(c.value) {
		c.value = nil
		c.truncated = true
		return
	}
	if c.value == nil {
		c.value = make([]byte, 0, c.limit)
	}
	c.value = append(c.value, value...)
}

func (c *responseBodyCapture) Result(complete bool) (string, bool) {
	if c.truncated || !complete {
		return "", c.truncated
	}
	if c.encoding == "" || c.encoding == "identity" {
		return base64.StdEncoding.EncodeToString(c.value), false
	}
	var reader io.ReadCloser
	var err error
	switch c.encoding {
	case "gzip":
		reader, err = gzip.NewReader(bytes.NewReader(c.value))
	case "deflate":
		// RFC deflate uses a zlib wrapper; some upstreams send raw DEFLATE.
		reader, err = zlib.NewReader(bytes.NewReader(c.value))
		if err != nil {
			reader = flate.NewReader(bytes.NewReader(c.value))
			err = nil
		}
	default:
		return "", false
	}
	if err != nil {
		return "", false
	}
	defer reader.Close()
	decoded, err := io.ReadAll(io.LimitReader(reader, responseBodyMaxBytes+1))
	if len(decoded) > responseBodyMaxBytes {
		c.value = nil
		c.truncated = true
		return "", true
	}
	if err != nil {
		return "", false
	}
	return base64.StdEncoding.EncodeToString(decoded), false
}

type responseBodyWriter struct{ capture *responseBodyCapture }

func (w responseBodyWriter) Write(value []byte) (int, error) {
	w.capture.Append(value)
	return len(value), nil
}

func responseBodyEncoding(headers http.Header) string {
	return strings.ToLower(strings.TrimSpace(strings.Join(headers.Values("Content-Encoding"), ",")))
}

func shouldCaptureResponseBody(headers http.Header, req *http.Request, capture *HistoryCapture) bool {
	contentType := headers.Get("Content-Type")
	if !shouldCaptureBody(contentType, req, capture) {
		return false
	}
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	// A configured whitelist may narrow these text types, never admit binaries.
	if !strings.HasPrefix(mediaType, "text/") && mediaType != "application/json" && mediaType != "application/xml" && !strings.HasSuffix(mediaType, "+json") && !strings.HasSuffix(mediaType, "+xml") {
		return false
	}
	for _, value := range headers.Values("Content-Disposition") {
		kind, params, err := mime.ParseMediaType(value)
		if err != nil || strings.EqualFold(kind, "attachment") {
			return false
		}
		if _, file := params["filename"]; file {
			return false
		}
	}
	switch responseBodyEncoding(headers) {
	case "", "identity", "gzip", "deflate":
		return true
	default:
		return false
	}
}
