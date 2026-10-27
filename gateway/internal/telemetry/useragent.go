package telemetry

import (
	"strings"
	"unicode"

	"yaginx-gateway/internal/proxy/event"

	"github.com/ua-parser/uap-go/uaparser"
)

// UserAgentParser converts raw User-Agent strings into stable client fields.
// The parser owns a bounded uap cache and is intended to run in asynchronous
// workers, never on the proxy request path.
type UserAgentParser struct {
	enabled bool
	parser  *uaparser.Parser
}

func NewUserAgentParser(enabled bool, cacheSize int) *UserAgentParser {
	if cacheSize < 1 {
		cacheSize = 1000
	}
	parser, err := uaparser.New(uaparser.WithCacheSize(cacheSize))
	if err != nil {
		return &UserAgentParser{enabled: enabled}
	}
	return &UserAgentParser{enabled: enabled, parser: parser}
}

func (p *UserAgentParser) Parse(raw string) (info event.ClientInfo) {
	info.ParseStatus = "unknown"
	defer func() {
		if recover() != nil {
			info = event.ClientInfo{ParseStatus: "unknown", ParseFailed: true}
		}
	}()
	if p == nil || !p.enabled {
		return event.ClientInfo{ParseStatus: "disabled"}
	}
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return event.ClientInfo{ParseStatus: "empty"}
	}
	if p.parser == nil {
		return event.ClientInfo{ParseStatus: "unknown", ParseFailed: true}
	}
	client := p.parser.Parse(raw)
	if client.UserAgent != nil {
		info.BrowserName = strings.TrimSpace(client.UserAgent.Family)
		if strings.EqualFold(info.BrowserName, "Other") {
			info.BrowserName = ""
		}
		info.BrowserVersion = versionString(client.UserAgent.Major, client.UserAgent.Minor, client.UserAgent.Patch)
		info.BrowserMajor = strings.TrimSpace(client.UserAgent.Major)
	}
	if client.Os != nil {
		info.OSName = strings.TrimSpace(client.Os.Family)
		if strings.EqualFold(info.OSName, "Other") {
			info.OSName = ""
		}
		info.OSVersion = versionString(client.Os.Major, client.Os.Minor, client.Os.Patch, client.Os.PatchMinor)
		info.OSMajor = strings.TrimSpace(client.Os.Major)
	}
	deviceFamily := ""
	if client.Device != nil {
		deviceFamily = strings.TrimSpace(client.Device.Family)
		if strings.EqualFold(deviceFamily, "Other") {
			deviceFamily = ""
		}
		info.DeviceBrand = strings.TrimSpace(client.Device.Brand)
		if strings.EqualFold(info.DeviceBrand, "Other") {
			info.DeviceBrand = ""
		}
		info.DeviceModel = strings.TrimSpace(client.Device.Model)
		if info.DeviceModel == "" || strings.EqualFold(info.DeviceModel, "Other") {
			info.DeviceModel = deviceFamily
		}
		if strings.EqualFold(info.DeviceModel, "Other") {
			info.DeviceModel = ""
		}
	}
	info.IsBot = isSpiderUserAgent(raw, info.BrowserName)
	if info.IsBot {
		info.BotName = botNameFromUA(raw, info.BrowserName)
	}
	info.DeviceType = classifyDevice(raw, info.OSName, deviceFamily)
	if info.IsBot || info.BrowserName != "" || info.OSName != "" || info.DeviceType != "unknown" {
		info.ParseStatus = "parsed"
	}
	return info
}

func versionString(parts ...string) string {
	values := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			break
		}
		values = append(values, part)
	}
	return strings.Join(values, ".")
}

func classifyDevice(raw, osName, deviceFamily string) string {
	value := strings.ToLower(raw)
	family := strings.ToLower(deviceFamily)
	switch {
	case strings.Contains(family, "tablet"), strings.Contains(value, "ipad"), strings.Contains(value, "tablet"), strings.Contains(value, "kindle"), strings.Contains(value, "silk/"):
		return "tablet"
	case strings.Contains(family, "console"), strings.Contains(value, "playstation"), strings.Contains(value, "xbox"), strings.Contains(value, "nintendo"):
		return "console"
	case strings.Contains(family, "tv"), strings.Contains(value, "smart-tv"), strings.Contains(value, "smarttv"), strings.Contains(value, "hbbtv"):
		return "smarttv"
	case strings.Contains(family, "mobile"), strings.Contains(value, "mobile"), strings.Contains(value, "iphone"), strings.Contains(value, "ipod"):
		return "mobile"
	case strings.Contains(family, "embedded"), strings.Contains(value, "embedded"):
		return "embedded"
	case strings.Contains(value, "android"):
		// Android tablets generally omit the Mobile token. Android TV is
		// handled above, so the remaining Android clients are tablets.
		return "tablet"
	case strings.Contains(strings.ToLower(osName), "windows"), strings.Contains(strings.ToLower(osName), "mac"), strings.Contains(strings.ToLower(osName), "linux"), strings.Contains(strings.ToLower(osName), "chrome os"):
		return "desktop"
	default:
		return "unknown"
	}
}

func botNameFromUA(raw, browser string) string {
	for _, token := range strings.FieldsFunc(raw, func(r rune) bool {
		return unicode.IsSpace(r) || strings.ContainsRune("()[]{};,", r)
	}) {
		name := strings.TrimSpace(strings.SplitN(token, "/", 2)[0])
		lower := strings.ToLower(name)
		if strings.Contains(lower, "bot") || strings.Contains(lower, "crawler") || strings.Contains(lower, "spider") || strings.Contains(lower, "slurp") || lower == "facebookexternalhit" {
			return name
		}
	}
	if browser != "" {
		return browser
	}
	return "bot"
}

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
	parser := NewUserAgentParser(enabled, cacheSize)
	return &userAgentClassifier{enabled: enabled, parser: parser.parser}
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
