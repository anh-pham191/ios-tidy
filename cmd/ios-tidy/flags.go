// cmd/ios-tidy/flags.go
package main

import (
	"flag"
	"strings"
)

// splitFlagsAndPositionals separates args into a flag-token slice (passed to
// fs.Parse) and a positionals slice (returned for the caller to consume in
// place of fs.Args()).
//
// Why this exists: flag.Parse stops at the first non-flag argument. So
// `ios-tidy apps clean com.example.app --dry-run` parses bundleID as the
// only positional, leaves `--dry-run` in fs.Args(), and SILENTLY IGNORES IT
// — proceeding to a real deletion after the y/N prompt. That is a
// data-loss-adjacent footgun for every subcommand that mixes flags with a
// positional argument.
//
// Approach: pre-scan args to classify each token using fs's declared flag
// set. A token is a "flag token" if it starts with `-` and matches a known
// flag name (with the leading dashes stripped, optionally with `=value`
// attached). For non-bool flags in `--name VALUE` form, the following arg
// is consumed as the flag's value. The `--` sentinel disables further flag
// parsing and is preserved in the flag stream. Everything else is a
// positional.
//
// Unknown flags are passed through to flag.Parse unchanged so the standard
// "flag provided but not defined" error path keeps working.
//
// The returned flagArgs ends with `--` so fs.Parse won't try to consume any
// trailing token as a flag. The caller MUST use the returned positionals
// rather than fs.Args() — after a successful Parse, fs.Args() will be empty
// because we routed positionals around it.
func splitFlagsAndPositionals(fs *flag.FlagSet, args []string) (flagArgs, positionals []string) {
	knownFlags := map[string]bool{}
	boolFlags := map[string]bool{}
	fs.VisitAll(func(f *flag.Flag) {
		knownFlags[f.Name] = true
		// flag.Value's IsBoolFlag is the convention for bool-shaped flags
		// that DON'T consume the next arg. The stdlib bool flag type
		// implements it; user-defined Value impls (like stringSliceFlag)
		// don't, which correctly leaves them treated as value-consuming.
		if bf, ok := f.Value.(interface{ IsBoolFlag() bool }); ok && bf.IsBoolFlag() {
			boolFlags[f.Name] = true
		}
	})

	afterDoubleDash := false
	for i := 0; i < len(args); i++ {
		a := args[i]
		if afterDoubleDash {
			positionals = append(positionals, a)
			continue
		}
		if a == "--" {
			// Preserve the sentinel in flagArgs so fs.Parse sees it and
			// stops flag parsing at the canonical boundary.
			flagArgs = append(flagArgs, a)
			afterDoubleDash = true
			continue
		}
		if !strings.HasPrefix(a, "-") || a == "-" {
			positionals = append(positionals, a)
			continue
		}
		// Strip leading dashes to identify the flag name. Handle --name=value.
		name := strings.TrimLeft(a, "-")
		if eq := strings.IndexByte(name, '='); eq >= 0 {
			name = name[:eq]
		}
		if !knownFlags[name] {
			// Unknown flag — leave for flag.Parse to error on (preserves
			// standard error messages and exit-code shape).
			flagArgs = append(flagArgs, a)
			continue
		}
		flagArgs = append(flagArgs, a)
		// Non-bool flag in `--name VALUE` form (no `=`): consume the next
		// arg as its value so we don't accidentally classify the value as
		// a positional.
		if !boolFlags[name] && !strings.Contains(a, "=") && i+1 < len(args) {
			flagArgs = append(flagArgs, args[i+1])
			i++
		}
	}
	// Append a `--` sentinel so fs.Parse never tries to interpret a trailing
	// token as a flag (it won't, because we removed all of them, but the
	// sentinel makes the intent explicit and is cheap).
	flagArgs = append(flagArgs, "--")
	return flagArgs, positionals
}
