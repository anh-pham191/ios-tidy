package main

// Version is the human-readable release tag, overridable at build time
// via:  go build -ldflags="-X main.Version=1.2.3" ./cmd/ios-tidy
// The "dev" default makes `ios-tidy --version` work in unbuilt clones.
var Version = "dev"
