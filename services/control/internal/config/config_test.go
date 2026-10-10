package config

import "testing"

func TestConfigurationBoundaries(t *testing.T) {
	tests := []struct {
		name, mode, listen, origin, ttl string
		valid                           bool
	}{
		{"local dev", "development", "127.0.0.1:8080", "http://127.0.0.1:8080", "12h", true},
		{"secure production", "production", "0.0.0.0:8080", "https://control.example.com", "12h", true},
		{"insecure production", "production", "127.0.0.1:8080", "http://localhost:8080", "12h", false},
		{"public dev listener", "development", "0.0.0.0:8080", "http://localhost:8080", "12h", false},
		{"public dev origin", "development", "127.0.0.1:8080", "https://control.example.com", "12h", false},
		{"origin path", "production", "127.0.0.1:8080", "https://control.example.com/", "12h", false},
		{"origin credentials", "production", "127.0.0.1:8080", "https://secret@control.example.com", "12h", false},
		{"unbounded expiry", "production", "127.0.0.1:8080", "https://control.example.com", "48h", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			values := map[string]string{"DATABASE_URL": "postgres://localhost/test", "CONTROL_MODE": tt.mode, "CONTROL_LISTEN": tt.listen, "CONTROL_ORIGIN": tt.origin, "SESSION_TTL": tt.ttl}
			_, err := Parse(func(k string) string { return values[k] })
			if (err == nil) != tt.valid {
				t.Fatalf("unexpected validity: %v", err)
			}
		})
	}
	_, err := Parse(func(string) string { return "" })
	if err == nil {
		t.Fatal("missing database accepted")
	}
}

func TestGeoPublisherConfiguration(t *testing.T) {
	values := map[string]string{"DATABASE_URL": "postgres://localhost/test", "CONTROL_MODE": "development", "CONTROL_LISTEN": "127.0.0.1:8080", "CONTROL_ORIGIN": "http://127.0.0.1:5173", "GEOIP_ROOT": "/private/geoip", "GEOIP_PUBLISH_ENABLED": "true"}
	cfg, err := Parse(func(k string) string { return values[k] })
	if err != nil || !cfg.GeoPublisher || cfg.GeoIPRoot != "/private/geoip" {
		t.Fatal("geo publisher configuration ignored", err)
	}
	values["GEOIP_PUBLISH_ENABLED"] = "bad"
	if _, err := Parse(func(k string) string { return values[k] }); err == nil {
		t.Fatal("invalid publisher option accepted")
	}
}
func TestAgentDistributionConfiguration(t *testing.T) {
	values := map[string]string{"DATABASE_URL": "postgres://localhost/test", "CONTROL_MODE": "production", "CONTROL_LISTEN": "0.0.0.0:8080", "CONTROL_ORIGIN": "https://dashboard.example.com", "CONTROL_PUBLIC_URL": "https://api.example.com", "AGENT_ASSET_ROOT": "/data/assets", "AGENT_RELEASE_INPUT": "/data/build", "PHP_ASSET_ROOT": "/data/php-assets", "PHP_RELEASE_INPUT": "/data/php-build"}
	cfg, err := Parse(func(k string) string { return values[k] })
	if err != nil || cfg.PublicURL != values["CONTROL_PUBLIC_URL"] || cfg.AgentAssetRoot != "/data/assets" || cfg.PHPAssetRoot != "/data/php-assets" || cfg.PHPReleaseInput != "/data/php-build" {
		t.Fatal(cfg, err)
	}
	for _, bad := range []string{"http://api.example.com", "https://user:pass@api.example.com", "https://api.example.com/path", "https://api.example.com:99999"} {
		values["CONTROL_PUBLIC_URL"] = bad
		if _, err = Parse(func(k string) string { return values[k] }); err == nil {
			t.Fatal("invalid API origin accepted", bad)
		}
	}
}
