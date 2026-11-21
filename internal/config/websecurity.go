package config

import (
	"fmt"
	"strings"
)

// CSP modes (ADR-0016).
const (
	CSPEnforce    = "enforce"
	CSPReportOnly = "report-only"
)

// WebSecurityConfig holds the response headers' settings (ADR-0016).
type WebSecurityConfig struct {
	// CSPMode is CSP_MODE: enforce (default) or report-only.
	CSPMode string
	// HSTS is on when cookies are Secure, i.e. the site is served over HTTPS.
	HSTS bool
}

func LoadWebSecurityConfig() (WebSecurityConfig, error) {
	c := WebSecurityConfig{CSPMode: strings.ToLower(strings.TrimSpace(GetEnv("CSP_MODE", CSPEnforce))), HSTS: GetEnvBool("COOKIE_SECURE", true)}
	if c.CSPMode != CSPEnforce && c.CSPMode != CSPReportOnly {
		return c, fmt.Errorf("CSP_MODE must be %s or %s", CSPEnforce, CSPReportOnly)
	}
	return c, nil
}
