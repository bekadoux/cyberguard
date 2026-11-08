package config

import (
	"strings"
	"testing"
	"time"
)

func TestAuthDefaultsAreSecure(t *testing.T) {
	t.Parallel()
	cfg, err := loadAuth(lookup(nil))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Environment != "production" || !cfg.CookieSecure || len(cfg.TrustedOrigins) != 0 || cfg.Lifetime != 8*time.Hour || cfg.IdleTimeout != 30*time.Minute {
		t.Fatal("unexpected authentication defaults")
	}
}

func TestDevelopmentAuthConfiguration(t *testing.T) {
	t.Parallel()
	cfg, err := loadAuth(lookup(map[string]string{
		"APP_ENV": "development", "SESSION_COOKIE_SECURE": "false",
		"TRUSTED_ORIGINS":  "http://127.0.0.1:5173, http://localhost:5173,http://127.0.0.1:5173",
		"SESSION_LIFETIME": "4h", "SESSION_IDLE_TIMEOUT": "15m",
	}))
	if err != nil {
		t.Fatal(err)
	}
	if cfg.CookieSecure || len(cfg.TrustedOrigins) != 2 || cfg.Lifetime != 4*time.Hour || cfg.IdleTimeout != 15*time.Minute {
		t.Fatal("development overrides were not loaded")
	}
}

func TestInvalidAuthConfiguration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ key, value string }{
		{"APP_ENV", ""}, {"APP_ENV", "test"}, {"SESSION_COOKIE_SECURE", "false"},
		{"SESSION_COOKIE_SECURE", "0"}, {"SESSION_LIFETIME", "0s"}, {"SESSION_LIFETIME", "48h"},
		{"SESSION_IDLE_TIMEOUT", ""}, {"SESSION_IDLE_TIMEOUT", "9h"},
		{"TRUSTED_ORIGINS", "*"}, {"TRUSTED_ORIGINS", "https://*.example.com"},
		{"TRUSTED_ORIGINS", "https://user:sensitive@example.com"},
		{"TRUSTED_ORIGINS", "https://example.com/path"}, {"TRUSTED_ORIGINS", "https://example.com/"},
		{"TRUSTED_ORIGINS", "https://example.com?secret=sensitive"}, {"TRUSTED_ORIGINS", "https://example.com?"},
		{"TRUSTED_ORIGINS", "https://example.com#fragment"}, {"TRUSTED_ORIGINS", "https://example.com:70000"},
		{"TRUSTED_ORIGINS", "https://example.com#"}, {"TRUSTED_ORIGINS", "https://example.com:"},
		{"TRUSTED_ORIGINS", "https://example.com,"}, {"TRUSTED_ORIGINS", "http://127.0.0.1:5173"},
		{"TRUSTED_PROXY_CIDRS", "*"}, {"TRUSTED_PROXY_CIDRS", "0.0.0.0/0"},
		{"TRUSTED_PROXY_CIDRS", "::/0"}, {"TRUSTED_PROXY_CIDRS", "10.0.0.1"},
		{"TRUSTED_PROXY_CIDRS", "10.0.0.0/24,"}, {"TRUSTED_PROXY_CIDRS", "::ffff:192.0.2.1/128"},
	} {
		t.Run(test.key+"/"+test.value, func(t *testing.T) {
			t.Parallel()
			_, err := loadAuth(lookup(map[string]string{test.key: test.value}))
			if err == nil || !strings.Contains(err.Error(), test.key) || strings.Contains(err.Error(), "sensitive") {
				t.Fatal("expected a safe error naming the invalid authentication setting")
			}
		})
	}
}

func TestTrustedProxyConfiguration(t *testing.T) {
	t.Parallel()
	cfg, err := loadAuth(lookup(map[string]string{"TRUSTED_PROXY_CIDRS": "10.0.0.2/32, fd00::2/128"}))
	if err != nil || len(cfg.TrustedProxies) != 2 {
		t.Fatal("explicit IPv4/IPv6 proxy addresses should be supported")
	}
	_, err = loadAuth(lookup(map[string]string{"TRUSTED_PROXY_CIDRS": strings.Repeat("10.0.0.2/32,", 32) + "10.0.0.2/32"}))
	if err == nil {
		t.Fatal("proxy trust list must be bounded")
	}
}

func TestProductionHTTPSOrigin(t *testing.T) {
	t.Parallel()
	cfg, err := loadAuth(lookup(map[string]string{"TRUSTED_ORIGINS": "https://cyberguard.example:8443"}))
	if err != nil || len(cfg.TrustedOrigins) != 1 {
		t.Fatal("an explicit HTTPS origin should be supported")
	}
	_, err = loadAuth(lookup(map[string]string{"APP_ENV": "development", "TRUSTED_ORIGINS": "http://example.com"}))
	if err == nil {
		t.Fatal("development HTTP exception must be loopback-only")
	}
}
