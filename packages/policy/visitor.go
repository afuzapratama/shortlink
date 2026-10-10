package policy

import (
	"github.com/ua-parser/uap-go/uaparser"
	"strings"
	"sync"
)

var parserOnce sync.Once
var parser *uaparser.Parser
var parserErr error

func Init() error {
	parserOnce.Do(func() { parser, parserErr = uaparser.New(uaparser.WithSort(false), uaparser.WithCacheSize(4096)) })
	return parserErr
}
func optional(value string) *string {
	if value == "" || value == "Other" {
		return nil
	}
	return &value
}
func Detect(ua string) Signals {
	bot, category, suspected := Crawler(ua)
	s := Signals{Country: "unknown", Device: "unknown", OS: "unknown", Browser: "unknown", KnownBot: bot, BotCategory: category, SuspectedAutomation: suspected, FeedStatus: "unavailable", TorStatus: "unavailable", ReputationStatus: "unavailable"}
	if ua == "" || len(ua) > 4096 || Init() != nil {
		return s
	}
	client := parser.Parse(ua)
	os := client.Os.Family
	browser := client.UserAgent.Family
	switch {
	case os == "iOS":
		s.OS = "ios"
	case strings.Contains(os, "Android"):
		s.OS = "android"
	case strings.Contains(os, "Windows"):
		s.OS = "windows"
	case os == "Mac OS X" || os == "Mac OS":
		s.OS = "macos"
	case strings.Contains(os, "Chrome OS"):
		s.OS = "chromeos"
	case strings.Contains(os, "Linux") || member(os, "Ubuntu|Debian|Fedora"):
		s.OS = "linux"
	case os != "Other":
		s.OS = "other"
	}
	switch {
	case strings.Contains(browser, "Edge"):
		s.Browser = "edge"
	case strings.Contains(browser, "Samsung Internet"):
		s.Browser = "samsung_internet"
	case strings.Contains(browser, "Opera"):
		s.Browser = "opera"
	case strings.Contains(browser, "Chrome"):
		s.Browser = "chrome"
	case strings.Contains(browser, "Firefox"):
		s.Browser = "firefox"
	case strings.Contains(browser, "Safari"):
		s.Browser = "safari"
	case browser != "Other":
		s.Browser = "other"
	}
	// Device is distinct from OS: Android tablets and iPads do not become mobile by OS alone.
	lower := strings.ToLower(ua)
	family := strings.ToLower(client.Device.Family)
	switch {
	case strings.Contains(lower, "ipad") || strings.Contains(family, "ipad") || strings.Contains(lower, "tablet") || strings.Contains(family, "tablet") || (s.OS == "android" && !strings.Contains(lower, "mobile")):
		s.Device = "tablet"
	case strings.Contains(lower, "iphone") || strings.Contains(lower, "ipod") || strings.Contains(lower, "mobile") || s.OS == "ios":
		s.Device = "mobile"
	case member(s.OS, "windows|macos|linux|chromeos"):
		s.Device = "desktop"
	}
	s.OSVersion = optional(client.Os.ToVersionString())
	s.BrowserVersion = optional(client.UserAgent.ToVersionString())
	s.DeviceBrand = optional(client.Device.Brand)
	s.DeviceModel = optional(client.Device.Model)
	return s
}
