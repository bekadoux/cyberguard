package config

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type AuthConfig struct {
	Environment    string
	CookieSecure   bool
	TrustedOrigins []string
	Lifetime       time.Duration
	IdleTimeout    time.Duration
	TrustedProxies []netip.Prefix
}

func loadAuth(lookup func(string) (string, bool)) (AuthConfig, error) {
	cfg := AuthConfig{Environment: "production", CookieSecure: true}
	if value, ok := lookup("APP_ENV"); ok {
		cfg.Environment = value
	}
	if cfg.Environment != "production" && cfg.Environment != "development" {
		return AuthConfig{}, errors.New("APP_ENV must be production or development")
	}
	if value, ok := lookup("SESSION_COOKIE_SECURE"); ok {
		if value != "true" && value != "false" {
			return AuthConfig{}, errors.New("SESSION_COOKIE_SECURE must be true or false")
		}
		cfg.CookieSecure = value == "true"
	}
	if !cfg.CookieSecure && cfg.Environment != "development" {
		return AuthConfig{}, errors.New("SESSION_COOKIE_SECURE=false requires APP_ENV=development")
	}
	var err error
	cfg.Lifetime, err = sessionDuration(lookup, "SESSION_LIFETIME", 8*time.Hour)
	if err != nil {
		return AuthConfig{}, err
	}
	cfg.IdleTimeout, err = sessionDuration(lookup, "SESSION_IDLE_TIMEOUT", 30*time.Minute)
	if err != nil {
		return AuthConfig{}, err
	}
	if cfg.IdleTimeout > cfg.Lifetime {
		return AuthConfig{}, errors.New("SESSION_IDLE_TIMEOUT must not exceed SESSION_LIFETIME")
	}
	if value, _ := lookup("TRUSTED_ORIGINS"); value != "" {
		seen := make(map[string]bool)
		for _, origin := range strings.Split(value, ",") {
			origin = strings.TrimSpace(origin)
			if !validOrigin(origin, cfg.Environment) {
				return AuthConfig{}, errors.New("TRUSTED_ORIGINS must contain exact HTTPS origins, or loopback HTTP origins in development")
			}
			if !seen[origin] {
				cfg.TrustedOrigins = append(cfg.TrustedOrigins, origin)
				seen[origin] = true
			}
		}
	}
	if value, _ := lookup("TRUSTED_PROXY_CIDRS"); value != "" {
		for _, network := range strings.Split(value, ",") {
			prefix, err := netip.ParsePrefix(strings.TrimSpace(network))
			if err != nil || prefix.Bits() == 0 || prefix.Addr().Is4In6() || len(cfg.TrustedProxies) >= 32 {
				return AuthConfig{}, errors.New("TRUSTED_PROXY_CIDRS must contain at most 32 explicit CIDRs, not catch-all or IPv4-mapped networks")
			}
			cfg.TrustedProxies = append(cfg.TrustedProxies, prefix.Masked())
		}
	}
	return cfg, nil
}

func validOrigin(value, environment string) bool {
	u, err := url.Parse(value)
	if err != nil || u.Hostname() == "" || u.User != nil || u.Path != "" || u.RawQuery != "" ||
		u.ForceQuery || strings.ContainsAny(value, "*#") || strings.HasSuffix(u.Host, ":") {
		return false
	}
	if u.Port() != "" && validateHTTPAddr(u.Host) != nil {
		return false
	}
	if u.Scheme == "https" {
		return true
	}
	if u.Scheme != "http" || environment != "development" {
		return false
	}
	ip := net.ParseIP(u.Hostname())
	return u.Hostname() == "localhost" || (ip != nil && ip.IsLoopback())
}

func sessionDuration(lookup func(string) (string, bool), name string, fallback time.Duration) (time.Duration, error) {
	value, ok := lookup(name)
	if !ok {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d < time.Minute || d > 24*time.Hour {
		return 0, fmt.Errorf("%s must be a duration between 1m and 24h", name)
	}
	return d, nil
}
