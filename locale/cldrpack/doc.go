// Package cldrpack loads and writes optional .cldrpack CLDR data bundles.
//
// The binary layout is private to this module; applications should use the
// returned cldr.Bundle and cldr.Services rather than depending on row IDs,
// offsets, chunks, or compression details.
package cldrpack
