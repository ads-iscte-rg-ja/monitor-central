package config

import (
	"fmt"
	"net/netip"
	"strings"
)

// Role is the part of the central an instance runs (ADR-0007).
type Role string

const (
	// RoleAll serves HTTP and gRPC ingestion and runs the background jobs.
	RoleAll Role = "all"
	// RoleWeb serves HTTP and gRPC ingestion only.
	RoleWeb Role = "web"
	// RoleWorker runs the background jobs and serves /health only.
	RoleWorker Role = "worker"
)

// Serves reports whether the instance serves the web UI and the ingestion API.
func (r Role) Serves() bool { return r != RoleWorker }

// RunsJobs reports whether the instance takes part in running background jobs.
func (r Role) RunsJobs() bool { return r != RoleWeb }

// ScalingConfig is what changes when the central runs as several instances.
type ScalingConfig struct {
	Role Role
	// TrustedProxies are the only peers whose X-Forwarded-For is believed.
	TrustedProxies []netip.Prefix
}

func LoadScalingConfig() (*ScalingConfig, error) {
	role := Role(GetEnv("APP_ROLE", string(RoleAll)))
	if role != RoleAll && role != RoleWeb && role != RoleWorker {
		return nil, fmt.Errorf("APP_ROLE must be all, web or worker")
	}
	c := &ScalingConfig{Role: role}
	for _, item := range strings.Split(GetEnv("TRUSTED_PROXIES", ""), ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		prefix, err := netip.ParsePrefix(item)
		if err != nil {
			addr, addrErr := netip.ParseAddr(item)
			if addrErr != nil {
				return nil, fmt.Errorf("TRUSTED_PROXIES must be a comma-separated list of IP addresses or CIDR ranges: %q", item)
			}
			prefix = netip.PrefixFrom(addr, addr.BitLen())
		}
		c.TrustedProxies = append(c.TrustedProxies, prefix.Masked())
	}
	return c, nil
}
