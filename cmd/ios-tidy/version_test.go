package main

import "testing"

func TestVersion_hasNonEmptyDefault(t *testing.T) {
	if Version == "" {
		t.Fatal("Version default must be non-empty so --version always has something to print")
	}
}
