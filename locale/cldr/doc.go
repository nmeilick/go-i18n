// Package cldr exposes optional CLDR data bundles and services.
//
// The package is the public boundary between semantic locale APIs and physical
// CLDR data storage. Applications can use the built-in lean bundle, generated
// application bundles, embedded .cldrpack data, or external .cldrpack files
// through the same Bundle and Services APIs.
//
// Normal applications usually do not need this package; locale.DefaultFormatter
// and locale.GeneratedDefaults use the built-in lean data. Applications that
// need explicit data-source ownership, hot reload, custom bundles, or external
// pack files can build a Services value and pass its Formatter and
// DefaultsProvider into their i18n runtime or locale resolver.
package cldr
