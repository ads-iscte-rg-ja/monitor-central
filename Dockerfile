ARG GO_IMAGE=golang:1.27.4-alpine
# Mesma versão que a diretiva `toolchain` do go.mod: o binário de produção é compilado com o Go
# que o CI testa e que o govulncheck analisa.
FROM ${GO_IMAGE} AS build

WORKDIR /src
ENV CGO_ENABLED=0 GOWORK=off

COPY go.mod go.sum ./
RUN go mod download

COPY . .
# Includes the embedded deployment SQL and all administrative commands.
RUN go build -trimpath -o /out/ ./cmd/...

FROM alpine:3.23 AS runtime

# Certificate roots for remote SMTP TLS, and Europe/Lisbon for alert rendering
# and the daily summary scheduler. BusyBox supplies wget for the health check.
RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app
COPY --from=build /out/ /app/bin/
COPY --from=build /src/frontend/ /app/frontend/

ENV PORT=3333 GRPC_PORT=3334 METRICS_ADDR=:9464 FRONTEND_PATH=/app/frontend
USER 65532:65532
EXPOSE 3333 3334 9464
CMD ["/app/bin/app"]
