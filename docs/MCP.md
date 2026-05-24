# ios-tidy MCP server

## What it is

`ios-tidy-mcp` is a stdio [Model Context Protocol](https://modelcontextprotocol.io)
server that lets Claude Desktop (or any MCP client) drive `ios-tidy`
programmatically. It exposes the same operations as the CLI as
discoverable, schema-described tools, and reuses the same internal seams
(`device.Lister`, `storage.Client`, `apps.Lister`, `crashlogs.Client`,
`apps.Prober`, `apps.ProbeStore`, `sandbox.Sandbox`) and per-UDID probe
cache. CLI and MCP runs are interchangeable: a probe written by one is
read by the other.

## Build

```bash
make build-mcp
```

This produces `bin/ios-tidy-mcp`. The same binary can also be installed
with `go install github.com/anh-pham191/ios-tidy/cmd/ios-tidy-mcp@latest`,
which drops it in `$(go env GOPATH)/bin/ios-tidy-mcp`.

## Wire it to Claude Desktop

1. Build or install the binary (see above) and note its absolute path.
2. Open `~/Library/Application Support/Claude/claude_desktop_config.json`
   (create it if it does not exist).
3. Merge in the snippet from
   [`docs/claude_desktop_config.example.json`](claude_desktop_config.example.json):

   ```json
   {
     "mcpServers": {
       "ios-tidy": {
         "command": "/ABSOLUTE/PATH/TO/bin/ios-tidy-mcp",
         "args": [],
         "env": {}
       }
     }
   }
   ```

   You MUST replace `/ABSOLUTE/PATH/TO/` with your actual install path —
   either `$(go env GOPATH)/bin/ios-tidy-mcp` if you used `go install`, or
   the repo's `bin/ios-tidy-mcp` if you built locally. Relative paths and
   `~` are not expanded by Claude Desktop's spawn path.
4. Quit Claude Desktop fully (Cmd-Q, not just close the window) and
   reopen it.
5. Verify: start a new chat, open the tools panel, and confirm the
   eleven `ios-tidy` tools are listed (plus `version`). If they are
   not, see Troubleshooting below.

## Tool catalog

Read-only tools (no safety gating):

- `devices_list` — List iPhones currently attached over USB.
- `storage` — Report device free/used bytes plus the per-app size table.
- `crashlogs_list` — List crash reports on the device, filterable by
  glob.
- `apps_list` — List installed user apps with their reported disk usage.
- `apps_probe` — Probe one or more bundles to see whether
  `mobile_house_arrest` will vend their sandboxes. Persists results to
  the shared probe cache.

Read-only guidance tools (v0.3.0; synthesis layer over the tools above):

- `storage_recommendations` — Combine free-space, top apps, crashlog
  total, and probe-cache state into a prioritised, human-actionable
  plan. Includes a `notTouchable` disclosure block that enumerates the
  data the host CANNOT clean (system caches, Safari/WebKit, Mail,
  Photos, the iOS "Other" bucket, Music/Podcasts downloads). The LLM
  caller should surface that block to the user so it does not promise
  cleanups it cannot deliver. Recommended starting tool for "my
  iPhone is full" requests.
- `apps_offload_candidates` — Size-ranked third-party app list with
  offload metadata. Smaller than `storage_recommendations` when you
  only need the app size table. Filters `com.apple.*`. Falls back to
  bundleID-ascending order with a stderr note when every reported
  size is zero (cold installation_proxy after device boot).
- `open_app_storage_settings` — Emits the iOS Settings deep-link URL
  for Settings → General → iPhone Storage → `<app>` plus step-by-step
  manual instructions. The tool CANNOT trigger Settings directly:
  go-ios v1.0.213 exposes no OpenURL capability, and ios-tidy will
  not launch arbitrary apps or send arbitrary URLs. Result `action`
  is always `manual-required`; the URL format is hard-coded to
  `prefs:root=STORAGE_MGMT_USAGE/<bundleID>`. Bundle ID must be
  printable ASCII (homoglyph defence) and not `com.apple.*`.

Destructive tools (each gated independently — see Safety model):

- `crashlogs_pull` — Copy crash reports from the device to a directory
  on the HOST machine. Writes to the host only; not destructive on the
  device.
- `crashlogs_clean` — Delete crash reports on the device. Defaults to
  dry-run; requires `confirm: true` to actually delete.
- `apps_clean` — Delete `tmp/`, `Library/Caches/`, and optionally
  `Documents/` inside one app's sandbox. Defaults to dry-run; requires
  `confirm_bundle_id == bundle_id` to delete, and an additional
  acknowledgement for `Documents/`.

## Safety model

Eight contracts that are non-bypassable from the MCP transport. There is
no `--yes` equivalent; every gate is its own argument and must be set
explicitly.

1. **Destructive tools default to dry-run.** `crashlogs_clean` and
   `apps_clean` both return plan-only results unless the caller passes
   the explicit confirmation argument. Argument-less calls always
   describe what would happen and never mutate.
2. **`crashlogs_clean` deletes only when `confirm: true`.** Any other
   value — omitted, `false`, or a truthy-looking string — is treated as
   a dry-run.
3. **`apps_clean` deletes only when `confirm_bundle_id == bundle_id`**
   (compared case-sensitively after `strings.TrimSpace`). Typos do not
   match. There is no shortcut argument.
4. **`apps_clean` with `include_documents: true` ALSO requires
   `i_understand_documents_are_unrecoverable: true`.** Both flags must
   be set, AND `confirm_bundle_id` must still match. There is no bypass.
   `Documents/` contents are not recoverable from this side.
5. **`apps_clean` refuses non-printable-ASCII bundle IDs.** A Cyrillic
   homoglyph (e.g. U+0430 `а` mimicking ASCII `a`) inside `bundle_id` or
   `confirm_bundle_id` is rejected before any device I/O. Apple bundle
   IDs are reverse-DNS and always ASCII; anything else is a typo or a
   homograph injection.
6. **`apps_clean` `dry_run` accepts only a real JSON bool.** A JSON
   string `"false"` does not disarm the safe default; the handler reads
   `dry_run` directly and treats any non-bool (string, number, null) as
   the safe `true`.
7. **`apps_clean` enforces a 5-minute probe-freshness TTL.** Even a
   Vended probe result is refused once it is at least 5 minutes old; the
   error message points back at `apps_probe`. Const, not configurable.
   CLI is unaffected (human-in-the-loop).
8. **`crashlogs_pull` `out` is path-restricted.** The destination must
   be an absolute path, a real directory (NOT a symlink), inside `$HOME`
   (or the `IOS_TIDY_MCP_PULL_ROOT` override), and NOT inside `.ssh`,
   `.gnupg`, `Library/LaunchAgents`, `Library/LaunchDaemons`,
   `Library/Keychains`, or `Library/Cookies`. Symlinks are refused even
   when their target is itself inside the allow-root, to neutralise
   TOCTOU swap attacks.

Every destructive tool result also stamps a `device: {udid, name}`
object so the caller can confirm the operation landed on the same
device they identified by name via `devices_list`.

## Probe gate

`apps_clean` refuses to touch any bundle that does not have a `vended`
outcome recorded in the probe cache, AND additionally requires that
result to be **less than 5 minutes old**. If the cache says `refused`,
`error`, or `unknown` — or if the bundle was never probed, or if the
last Vended result has aged out — the tool errors with a message
pointing back at `apps_probe`. Run:

```
apps_probe bundles=["com.example.myapp"]
```

(or `apps_probe all=true` to enumerate every installed user app) to
populate the cache first.

The probe cache lives at
`~/Library/Application Support/ios-tidy/probes/<UDID>.json` and is
shared with the CLI. A probe written by `ios-tidy apps probe` from a
terminal is visible to the MCP server's `apps_clean`, and vice versa.

## Limits inherited from go-ios

These are platform-level limits the MCP layer cannot work around. See
[RESEARCH.md](../RESEARCH.md) for the full feasibility study.

- **macOS 26 (Tahoe) pair-record TCC failures** — go-ios
  [#710](https://github.com/danielpaulus/go-ios/issues/710). The trust
  check inside `devices_list` may fail with a "pair-record path denied"
  error on Tahoe. Downgrade to macOS 14/15 or wait for an upstream fix.
- **iOS 17.1+ `house_arrest` sporadic refusals** — go-ios
  [#653](https://github.com/danielpaulus/go-ios/issues/653). `apps_probe`
  occasionally returns `refused` for bundles that vend fine on a second
  attempt. Re-run the probe.

## Troubleshooting

### Claude Desktop does not see the server

- Check the spawn log: `~/Library/Logs/Claude/mcp*.log` will contain any
  error from launching the binary (e.g. `ENOENT`, missing execute
  permission).
- Confirm the binary is executable: `chmod +x bin/ios-tidy-mcp` if
  needed.
- Confirm the path in `claude_desktop_config.json` is absolute and
  resolves; `~` and relative paths are not expanded by Claude Desktop's
  spawn path.
- Quit Claude Desktop fully (Cmd-Q) and reopen — config is only read at
  startup.

### A tool returns `"no devices attached"`

The iPhone is unplugged, not trusted, or `usbmuxd` cannot see it. Run
`ios-tidy devices` from the terminal to confirm. If the CLI sees the
device but the MCP tool does not, both are using the same
`device.Lister` so the most likely cause is that the device became
detached between calls — re-plug and retry.

### Every `apps_clean` call is refused with "not been confirmed as vended"

The probe cache is empty for this device, or every recorded bundle is
`refused`/`error`. Run `apps_probe all=true` to populate it, then
inspect the result. Only bundles with `outcome: "vended"` are eligible
for `apps_clean`.

### `apps_clean` errors with `"open sandbox ... daemon now refuses"`

The probe cache says `vended` but `house_arrest` has changed its mind
(common on iOS 17.1 — see go-ios #653). Re-run `apps_probe` for the
affected bundle to refresh the cache; if the new outcome is `refused`,
the daemon won't let us in this session.
