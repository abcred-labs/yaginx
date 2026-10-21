package telemetry

import (
	"strings"

	"github.com/ua-parser/uap-go/uaparser"
)

// userAgentClassifier uses the maintained ua-parser regex corpus, matching the
// previous service's family-level browser/OS data model. Its own bounded cache is
// deliberately owned by the asynchronous aggregation worker, never proxy I/O.
type userAgentClassifier struct {
	enabled bool
	parser  *uaparser.Parser
}

type uaDimension struct {
	spider  bool
	browser string
	os      string
}

func newUserAgentClassifier(enabled bool, cacheSize int) *userAgentClassifier {
	if cacheSize < 1 {
		cacheSize = 1000
	}
	parser, err := uaparser.New(uaparser.WithCacheSize(cacheSize))
	if err != nil {
		return &userAgentClassifier{}
	}
	return &userAgentClassifier{enabled: enabled, parser: parser}
}

func (c *userAgentClassifier) parse(raw string) uaDimension {
	if c == nil || !c.enabled || c.parser == nil || strings.TrimSpace(raw) == "" {
		return uaDimension{}
	}
	client := c.parser.Parse(raw)
	result := uaDimension{}
	if client.UserAgent != nil {
		result.browser = strings.TrimSpace(client.UserAgent.Family)
	}
	if client.Os != nil {
		result.os = strings.TrimSpace(client.Os.Family)
		if major := strings.TrimSpace(client.Os.Major); major != "" && result.os != "" {
			result.os += "-" + major
		}
	}
	result.spider = isSpiderUserAgent(raw, result.browser)
	return result
}

func isSpiderUserAgent(raw, family string) bool {
	value := strings.ToLower(raw + " " + family)
	for _, marker := range []string{"bot", "spider", "crawler", "slurp", "facebookexternalhit"} {
		if strings.Contains(value, marker) {
			return true
		}
	}
	return false
}
