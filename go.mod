module monitor-central

go 1.26.0

toolchain go1.27.4

require (
	github.com/alexedwards/scs/postgresstore v0.0.0-20251002162104-209de6e426de
	github.com/alexedwards/scs/v2 v2.9.0
	github.com/go-chi/chi/v5 v5.0.11
	github.com/lib/pq v1.11.1
	github.com/wneessen/go-mail v0.7.2
	golang.org/x/crypto v0.59.0
)

require (
	github.com/ads-iscte-rg-ja/monitor-api v1.0.0
	github.com/prometheus/client_golang v1.25.0
	golang.org/x/text v0.44.0
	google.golang.org/genproto/googleapis/rpc v0.0.0-20260817212433-ac3dfec99bb1
	google.golang.org/grpc v1.85.0
	google.golang.org/protobuf v1.36.12
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/prometheus/client_model v0.6.3 // indirect
	github.com/prometheus/common v0.72.0 // indirect
	github.com/prometheus/procfs v0.22.0 // indirect
	golang.org/x/net v0.63.0 // indirect
	golang.org/x/sys v0.50.0 // indirect
)
