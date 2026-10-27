package telemetry

import (
	"context"
	"testing"
)

func TestUserAgentParserClassifiesCommonClients(t *testing.T) {
	parser := NewUserAgentParser(true, 8)
	tests := []struct {
		name, raw, browser, os, device string
		bot                            bool
	}{
		{"chrome desktop", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36", "Chrome", "Windows", "desktop", false},
		{"safari mobile", "Mozilla/5.0 (iPhone; CPU iPhone OS 17_1 like Mac OS X) AppleWebKit/605.1.15 Version/17.1 Mobile/15E148 Safari/604.1", "Mobile Safari", "iOS", "mobile", false},
		{"google bot", "Mozilla/5.0 (compatible; Googlebot/2.1; +http://www.google.com/bot.html)", "Googlebot", "", "unknown", true},
		{"curl", "curl/8.0.1", "curl", "", "unknown", false},
		{"empty", "", "", "", "", false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := parser.Parse(test.raw)
			if got.BrowserName != test.browser || got.OSName != test.os || got.DeviceType != test.device || got.IsBot != test.bot {
				t.Fatalf("got browser=%q os=%q device=%q bot=%v status=%q", got.BrowserName, got.OSName, got.DeviceType, got.IsBot, got.ParseStatus)
			}
		})
	}
}

func TestUserAgentParserNormalizesUnknownAndCustomBot(t *testing.T) {
	parser := NewUserAgentParser(true, 8)
	unknown := parser.Parse("garbage ???")
	if unknown.BrowserName != "" || unknown.ParseStatus != "unknown" || unknown.DeviceType != "unknown" {
		t.Fatalf("unknown UA = %#v", unknown)
	}

	customBot := parser.Parse("Mozilla/5.0 (compatible; AcmeCrawler/1.0; +https://example.invalid/bot)")
	if !customBot.IsBot || customBot.BotName != "AcmeCrawler" || customBot.DeviceType == "bot" || customBot.ParseStatus != "parsed" {
		t.Fatalf("custom bot = %#v", customBot)
	}
}

func TestUserAgentParserClassifiesAndroidTablet(t *testing.T) {
	parser := NewUserAgentParser(true, 8)
	got := parser.Parse("Mozilla/5.0 (Linux; Android 13; SM-X700 Build/TP1A) AppleWebKit/537.36 Chrome/120.0.0.0 Safari/537.36")
	if got.DeviceType != "tablet" {
		t.Fatalf("android tablet device=%q, want tablet; full=%#v", got.DeviceType, got)
	}
}

func TestVersionStringIncludesOSPatchMinor(t *testing.T) {
	if got := versionString("17", "1", "2", "3"); got != "17.1.2.3" {
		t.Fatalf("version=%q, want 17.1.2.3", got)
	}
}

func TestUserAgentParserCanBeDisabled(t *testing.T) {
	got := NewUserAgentParser(false, 1).Parse("Mozilla/5.0 Chrome/120.0")
	if got.ParseStatus != "disabled" {
		t.Fatalf("parse status=%q, want disabled", got.ParseStatus)
	}
}

type captureHistoryStore struct{ events []Event }

func (s *captureHistoryStore) FlushRequestHistory(_ context.Context, events []Event) error {
	s.events = append([]Event(nil), events...)
	return nil
}

func TestHistorySinkParsesWithoutChangingProxyEventPath(t *testing.T) {
	store := &captureHistoryStore{}
	sink := HistorySink{Store: store, Parser: NewUserAgentParser(true, 4)}
	event := Event{UserAgent: "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Chrome/120.0"}
	if err := sink.Flush(context.Background(), []Event{event}); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 || store.events[0].UserAgentInfo.BrowserName != "Chrome" || store.events[0].UserAgentInfo.ParseStatus != "parsed" {
		t.Fatalf("history event client info = %#v", store.events)
	}
}

func TestHistorySinkPersistsWhenParserFails(t *testing.T) {
	store := &captureHistoryStore{}
	// A nil underlying parser simulates initialization failure without putting
	// an invalid User-Agent into the proxy path.
	sink := HistorySink{Store: store, Parser: &UserAgentParser{enabled: true}}
	if err := sink.Flush(context.Background(), []Event{{UserAgent: "broken"}}); err != nil {
		t.Fatal(err)
	}
	if len(store.events) != 1 || !store.events[0].UserAgentInfo.ParseFailed || store.events[0].UserAgentInfo.ParseStatus != "unknown" {
		t.Fatalf("failed parser event = %#v", store.events)
	}
}
