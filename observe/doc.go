// Package observe provides dependency-light observability primitives shared by
// go-i18n packages.
//
// The package intentionally does not import logging, metrics, tracing, HTTP, or
// OpenTelemetry packages. Applications adapt events to their own observability
// stack.
package observe
