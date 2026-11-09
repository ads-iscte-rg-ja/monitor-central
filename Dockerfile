ARG GO_IMAGE=golang:1.25-alpine
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

ENV PORT=3333 FRONTEND_PATH=/app/frontend
USER 65532:65532
EXPOSE 3333
CMD ["/app/bin/app"]
