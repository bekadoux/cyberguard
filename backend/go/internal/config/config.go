// Package config loads and validates the API's environment configuration.
package config

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	HTTPAddr            string
	IdentityDatabaseURL string
	IncidentDatabaseURL string
	DatabaseTimeout     time.Duration
	ShutdownTimeout     time.Duration
}

func Load() (Config, error) {
	return load(os.LookupEnv)
}

func load(lookup func(string) (string, bool)) (Config, error) {
	var cfg Config
	cfg.HTTPAddr = "127.0.0.1:8080"
	if value, ok := lookup("HTTP_ADDR"); ok {
		cfg.HTTPAddr = value
	}
	if err := validateHTTPAddr(cfg.HTTPAddr); err != nil {
		return Config{}, err
	}

	var err error
	cfg.IdentityDatabaseURL, err = databaseURL(lookup, "IDENTITY_DATABASE_URL")
	if err != nil {
		return Config{}, err
	}
	cfg.IncidentDatabaseURL, err = databaseURL(lookup, "INCIDENT_DATABASE_URL")
	if err != nil {
		return Config{}, err
	}
	cfg.DatabaseTimeout, err = duration(lookup, "DATABASE_TIMEOUT", 3*time.Second)
	if err != nil {
		return Config{}, err
	}
	cfg.ShutdownTimeout, err = duration(lookup, "SHUTDOWN_TIMEOUT", 10*time.Second)
	if err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func validateHTTPAddr(addr string) error {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return errors.New("HTTP_ADDR must be a host:port address")
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		return errors.New("HTTP_ADDR must use a port between 1 and 65535")
	}
	return nil
}

func databaseURL(lookup func(string) (string, bool), name string) (string, error) {
	value, ok := lookup(name)
	if !ok || strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", name)
	}
	u, err := url.Parse(value)
	// Never wrap URL parsing errors: they can contain database credentials.
	if err != nil || (u.Scheme != "postgres" && u.Scheme != "postgresql") ||
		u.Hostname() == "" || u.User == nil || u.User.Username() == "" ||
		strings.Trim(u.Path, "/ ") == "" || u.Fragment != "" {
		return "", fmt.Errorf("%s must be a PostgreSQL URL with host, user and database", name)
	}
	if port := u.Port(); port != "" {
		n, err := strconv.Atoi(port)
		if err != nil || n < 1 || n > 65535 {
			return "", fmt.Errorf("%s has an invalid port", name)
		}
	}
	return value, nil
}

func duration(lookup func(string) (string, bool), name string, fallback time.Duration) (time.Duration, error) {
	value, ok := lookup(name)
	if !ok {
		return fallback, nil
	}
	d, err := time.ParseDuration(value)
	if err != nil || d <= 0 || d > time.Minute {
		return 0, fmt.Errorf("%s must be a positive duration no greater than 1m", name)
	}
	return d, nil
}
