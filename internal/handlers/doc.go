// Package handlers is the HTTP presentation layer of the central (layered
// style, ADR-0019): it parses requests, calls the services through small
// interfaces (ADR-0014) and renders templates or JSON.
//
// It may use services, models and templating. It must not use repositories
// or database: data access belongs to the repository layer (depguard rule
// central-handlers in monitor-platform/lint/.golangci.yml). A change of
// presentation therefore stays here and in frontend/ (scenario MO3).
package handlers
