// Package iosbackend contains the only code in ios-tidy that imports
// github.com/danielpaulus/go-ios. Every other package depends on the
// abstract seam interfaces in internal/device, internal/storage, etc.,
// and is unit-tested against fakes from those packages.
//
// Why this discipline: go-ios pulls in a heavy graph (semver, plist,
// pkcs12, sirupsen/logrus, ...) and its API surface is concrete
// structs, not interfaces, so it cannot be mocked without an adapter.
// Keeping the dependency at this single boundary makes the rest of
// the project easy to test, easy to read, and free to swap libraries
// later if needed (see RESEARCH.md §9).
package iosbackend
