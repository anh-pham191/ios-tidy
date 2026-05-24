# ios-tidy

> A small, honest, USB-C-only iPhone storage cleanup CLI for macOS. Crash logs, per-app sandbox tmp/Caches/Documents for apps the device chooses to vend, and full app uninstall. Nothing more.

## What it does

- **Lists connected iPhones** with name, model, iOS version, UDID, trust state.
- **Reports device storage** plus a per-user-app size breakdown.
- **Lists, pulls and deletes crash logs** — the single biggest reliable win on most devices (100 MB – 2 GB typical).
- **Probes each user app** to see whether iOS's `mobile_house_arrest` daemon will let us touch its sandbox, and caches the answer per device.
- **Cleans per-app `tmp/`, `Library/Caches/` and (with strict confirmation) `Documents/`** for apps the probe confirmed as vended.
- All destructive operations support `--dry-run` and prompt before deleting.

## What it CANNOT do

These limits come from iOS itself, not from any library. They apply equally to every non-jailbroken USB-C cleaner.

- **Clear system caches** outside an app sandbox (`/private/var/mobile/Library/Caches/com.apple.*`). No service exposes them. [RESEARCH.md §5.1]
- **Clear Safari / WebKit caches.** Owned by system app sandboxes the daemon refuses to vend. [RESEARCH.md §5.2]
- **Clear Mail attachments.** Same — `com.apple.mobilemail`'s sandbox is not vended. [RESEARCH.md §5.3]
- **Touch the "Other" / "System Data" bucket.** Apple does not decompose this even on-device beyond Settings' opaque label. [RESEARCH.md §5.4]
- **Offload an app while keeping its data.** `installation_proxy` only exposes Install/Upgrade/Uninstall. "Keep data, drop binary" is iCloud-mediated with no public service. [RESEARCH.md §5.5]
- **Clean per-app Caches for App Store apps that the daemon refuses to vend.** On iOS 17+, `VendContainer` typically only succeeds for apps signed with `get-task-allow` (TestFlight, Xcode-installed, sideloaded). Vanilla App Store apps are commonly refused. `ios-tidy apps probe` tells you which is which on your device. [RESEARCH.md §3, §5.6]
- **Control iCloud** (Optimize Photos, iCloud Drive offload). [RESEARCH.md §5.7]
- **Delete Music or Podcasts downloaded media.** Files are reachable via AFC but tied to a CoreData DB that AFC can't update — deletion creates orphans. [RESEARCH.md §5.8]
- **Delete photos.** AFC can reach `/var/mobile/Media/DCIM` but deleting via AFC bypasses Photos.app and corrupts the Photos.sqlite database. Use Photos.app or PhotoKit on the host. [RESEARCH.md §4]

If a tool on the internet claims to do any of the above without jailbreak, it's either lying or doing something dangerous.

## Install

### From source (Go 1.23+)

```bash
go install github.com/anh-pham191/ios-tidy/cmd/ios-tidy@latest
```

The binary lands in `$(go env GOPATH)/bin/ios-tidy`. Make sure that's on your `PATH`.

### Homebrew

A Homebrew tap is planned post-launch. Until then, use `go install`.

## Quick start

```bash
# 1. See what's plugged in.
ios-tidy devices

# 2. See where storage is going.
ios-tidy storage

# 3. See what crash logs you could clean — but don't clean yet.
ios-tidy crashlogs clean --dry-run

# 4. Probe which apps the device will let us touch.
ios-tidy apps probe --all

# 5. Clean an app's caches (TestFlight / dev-signed / sideloaded apps usually work).
ios-tidy apps clean com.example.myapp --dry-run
ios-tidy apps clean com.example.myapp
```

## Commands

### `ios-tidy devices`

List connected iPhones.

```bash
ios-tidy devices
ios-tidy devices --json
```

### `ios-tidy storage [--device UDID] [--limit N] [--json]`

Show free/total volume bytes and a per-user-app size table.

```bash
ios-tidy storage
ios-tidy storage --device 00008110-001A1B2C3D4E5F6G --limit 20
ios-tidy storage --json
```

The free/total numbers are AFC-reported and may skew from Settings by a few hundred MB. [RESEARCH.md §7.3]

### `ios-tidy crashlogs list [--device UDID] [--pattern GLOB] [--json]`

List crash logs.

```bash
ios-tidy crashlogs list
ios-tidy crashlogs list --pattern 'Safari-*'
```

### `ios-tidy crashlogs pull --out DIR [--device UDID] [--pattern GLOB] [--force]`

Copy crash logs to a host directory.

```bash
ios-tidy crashlogs pull --out ./crashlogs
ios-tidy crashlogs pull --out ./crashlogs --pattern '*.ips' --force
```

### `ios-tidy crashlogs clean [--device UDID] [--pattern GLOB] [--dry-run] [--yes]`

Delete crash logs.

```bash
ios-tidy crashlogs clean --dry-run
ios-tidy crashlogs clean
ios-tidy crashlogs clean --yes  # skip the y/N prompt; still prints the plan
```

### `ios-tidy apps list [--device UDID] [--json]`

List installed user apps with their reported sizes.

```bash
ios-tidy apps list
ios-tidy apps list --json
```

`DynamicDiskUsage` may be zero for cold apps. Launch the app once and try again. [RESEARCH.md §7.4]

### `ios-tidy apps probe [--device UDID] [--bundle ID...] [--all] [--timeout 5s] [--json]`

Probe each bundle ID to see whether `mobile_house_arrest` will vend its sandbox. Results are cached at `~/Library/Application Support/ios-tidy/probes/<UDID>.json`.

```bash
ios-tidy apps probe --all
ios-tidy apps probe --bundle com.example.myapp --bundle org.mozilla.ios.Firefox
```

Outcomes: `vended` (we can touch its sandbox), `refused` (daemon said no — typical for App Store apps without `get-task-allow`), `error` (transport failure — retry), `unknown` (not probed yet).

### `ios-tidy apps clean BUNDLE_ID [--device UDID] [--dry-run] [--yes] [--include-tmp] [--include-caches] [--include-documents]`

Clean a per-app sandbox. **Refuses to run unless `apps probe` has confirmed `vended` for this bundle.**

Default targets: `tmp/` and `Library/Caches/`. If you pass any of `--include-tmp`, `--include-caches`, `--include-documents`, the defaults are switched off and only your explicit choices are included.

```bash
# Dry-run first — shows what would be deleted, never mutates.
ios-tidy apps clean com.example.myapp --dry-run

# Standard interactive flow.
ios-tidy apps clean com.example.myapp

# tmp only.
ios-tidy apps clean com.example.myapp --include-tmp

# Both file-cache targets explicit, Documents OFF — locks in the
# "explicit flags REPLACE defaults" rule. Identical effective targets
# to the bare `ios-tidy apps clean com.example.myapp` form, but spelled
# out for scripting clarity.
ios-tidy apps clean com.example.myapp --include-tmp --include-caches

# Documents — extra strict: type the bundle ID exactly to confirm.
# --yes does NOT bypass this typed-bundle-ID gate.
ios-tidy apps clean com.example.myapp --include-documents
```

The Documents flow asks you to retype the bundle ID exactly (case-sensitive) before any file is deleted. This is by design: Documents holds user data and is not recoverable from this side.

## Troubleshooting

### "no device connected" / "multiple devices connected"

- Plug the iPhone in with a known-good USB-C cable. `ios-tidy devices` should show it.
- If two phones are plugged in, pass `--device <UDID>` from the `ios-tidy devices` output.

### "Trust this computer" dialog won't go away

On the device, accept the dialog. If it doesn't appear, unplug, reboot the phone, plug back in. macOS `usbmuxd` is a stock LaunchDaemon — you don't need Homebrew `libimobiledevice`.

### macOS Tahoe (macOS 26) — pair-record access blocked by TCC

`go-ios` has an open issue ([#710](https://github.com/danielpaulus/go-ios/issues/710)) on macOS 26 Tahoe where TCC blocks reading the pair record. The documented `--pair-record-path` workaround is reported as not working as of 2026-05-23. If you're on Tahoe and get `failed to read pair record` errors, the current options are: downgrade to macOS 14/15, or wait for an upstream fix. [RESEARCH.md §6]

### `connect afc service failed` on iOS 17.1+

`go-ios` issue [#653](https://github.com/danielpaulus/go-ios/issues/653) documents sporadic `house_arrest` failures on iOS 17.1+. `ios-tidy` classifies these as transport errors (vs policy refusals) — retry the same command a few times. If it persists, the daemon may genuinely refuse this bundle; `apps probe` will record it as `refused`. [RESEARCH.md §3]

### Old probe cache files no longer load after upgrading

The probe cache JSON schema settled on `bundleID` (capital ID) across all subcommands as of this release. If you upgraded across that change, re-run `ios-tidy apps probe` to rebuild `~/Library/Application Support/ios-tidy/probes/<UDID>.json`.

### `apps clean` says "not been confirmed as vended"

The probe gate refuses to touch any bundle ID that hasn't been recorded as `vended` in the probe store. Run:

```bash
ios-tidy apps probe --bundle <BUNDLE_ID>
```

If the result is `refused`, the daemon won't let us into this app's sandbox. Try Settings → General → iPhone Storage → <app> → Offload App on the device, or use the App Store's "Delete and Reinstall" flow.

If the result is `vended` but `apps clean` then fails to open the sandbox, the probe may be stale — re-run `apps probe` to refresh.

### `DynamicDiskUsage` reads zero for an app I know is huge

Open the app on the device, wait a few seconds, then re-run `apps list` or `storage`. Cold apps sometimes report zero. [RESEARCH.md §7.4]

## Development

### Run unit tests

```bash
make test
```

Equivalent to `go test ./... -race`. Covers everything except `internal/iosbackend/*_device_test.go`.

### Run device integration tests

```bash
IOS_TIDY_TEST_UDID=<your-udid> make test-device
```

Equivalent to `go test -tags=device ./internal/iosbackend/...`. Each integration test `t.Skip`s if `IOS_TIDY_TEST_UDID` is unset, so the `make` target is safe to leave wired.

Destructive integration tests additionally require:

```bash
export IOS_TIDY_TEST_ALLOW_DESTRUCTIVE=1
export IOS_TIDY_TEST_SENTINEL_BUNDLE_ID=com.your-org.your-test-app
```

The sentinel bundle must be an app you have installed AND consent to having tmp/ files written into and deleted. Pick a TestFlight or Xcode-built app you control.

### Coverage

```bash
make test-cover
```

Targets ≥ 80% coverage on every `internal/*` package other than `internal/iosbackend/` (which is integration-tested on real hardware).

### Build a distributable binary

```bash
make build  # → ./bin/ios-tidy
```

Build flags: `-trimpath -ldflags="-s -w -X main.Version=<git-describe>"`.

### Lint

```bash
make lint
```

Runs `go vet ./...` plus `staticcheck ./...` if `staticcheck` is on `PATH`.

## MCP server (drive from Claude Desktop)

`ios-tidy` ships with a sibling binary `ios-tidy-mcp` that speaks the
[Model Context Protocol](https://modelcontextprotocol.io) over stdio.
Wire it into Claude Desktop and an LLM can drive `apps_probe` →
`apps_clean --dry-run` → `apps_clean` (with explicit re-confirmation)
the same way you would by hand.

Build:

```bash
make build-mcp
```

Wire it up: see [docs/MCP.md](docs/MCP.md) for the Claude Desktop config
snippet and the safety contract for destructive tools.

## License

MIT. See `LICENSE`.
