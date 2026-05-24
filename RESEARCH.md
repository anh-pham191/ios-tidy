# RESEARCH.md — iOS storage reclaim over USB-C from macOS

Purpose: document, with sources, what a non-jailbroken iPhone over USB-C will let
a host Mac inspect and delete. Used to bound the scope of `ios-tidy` honestly.

Investigation date: 2026-05-23.

---

## 1. Pinned library context

### Go — `github.com/danielpaulus/go-ios`

- Branch: `main`
- Commit SHA: `d596a56a679978c65c6ff8aea18eb06b55a3435e` (2026-05-07)
- Latest release: `v1.0.213` (2026-05-07)
- Cadence: 8 releases in the trailing ~6 weeks; actively maintained
- 2.1k stars, 171 open issues. License: MIT.

### Python — `github.com/doronz88/pymobiledevice3`

- Branch: `master`
- Commit SHA: `f3fa3bbe29567bee4ad15c4bc95b8d694ce69779` (2026-05-22)
- Latest release: `v9.13.0` (2026-05-23, same day as this report)
- Cadence: roughly weekly point releases; very actively maintained
- 2.3k stars, 97 open issues. **License: GPL-3.0-or-later.**

---

## 2. Service capability matrix

Verified against source for both libraries. Mutating method signatures pasted
verbatim in the per-language sections that follow.

| Service | LIST | READ | DELETE | Path / scope | Go availability | Python availability |
|---|:---:|:---:|:---:|---|:---:|:---:|
| `crashreportcopymobile` (+ `crashreportmover` trigger) | ✓ | ✓ | ✓ | `/var/mobile/Library/Logs/CrashReporter` | ✓ | ✓ |
| `com.apple.afc` (default AFC) | ✓ | ✓ | ✓ | `/var/mobile/Media` (DCIM, Books, Downloads, Recordings, iTunes_Control, PublicStaging, MediaAnalysis…) | ✓ | ✓ |
| `house_arrest` → AFC over app sandbox | ✓ | ✓ | ✓ | per-app container, *when the daemon vends it* (see §3) | ✓ (`VendContainer` only) | ✓ (`VendContainer` + `VendDocuments`) |
| `installation_proxy` | ✓ | ✓ | ✓ uninstall only | `BrowseUserApps` / `BrowseSystemApps` / `Uninstall` | ✓ | ✓ |
| `diagnostics_relay` / `DiagnosticsService` | n/a | ✓ | ✗ | IORegistry, battery, reboot. **No storage breakdown.** MobileGestalt deprecated on iOS ≥ 17.4. | ✓ | ✓ |
| `syslog_relay` / `OsTraceService` | n/a | ✓ stream | ✗ | logs only | ✓ | ✓ |
| `springboardservices` | ✓ icons | ✓ | ✗ | home-screen layout + icon PNGs | ✓ | ✓ |
| `misagent` | ✓ | ✓ | ✓ profiles | provisioning profiles — trivial size, ignore | ✓ | ✓ |
| `mobile_image_mounter` | ✓ | ✓ | ✓ unmount | Developer Disk Image — out of scope for cleanup | ✓ | ✓ |
| CoreDevice `FileServiceService` (iOS 17+) | ✓ | ✓ | **no delete verb in source** | adds `APP_GROUP_DATA_CONTAINER` inspection | ✗ (RSD support thin in go-ios) | ✓ (read-only via tunneld) |
| AFC2 (`com.apple.afc2`) | — | — | — | jailbreak-only; not present on stock iOS | n/a | n/a |

### Go signatures (verbatim from source at the pinned SHA)

```go
// ios/afc/client.go
func New(d ios.DeviceEntry) (*Client, error)
func NewFromConn(d ios.DeviceConnectionInterface) *Client
func (c *Client) List(p string) ([]string, error)
func (c *Client) Stat(s string) (FileInfo, error)
func (c *Client) WalkDir(p string, f WalkFunc) error
func (c *Client) Remove(p string) error
func (c *Client) RemoveAll(p string) error
func (c *Client) Pull(srcPath, dstPath string) error
func (c *Client) PullSingleFile(srcPath, dstPath string) error
func (c *Client) Push(srcPath, dstPath string) error
func (c *Client) DeviceInfo() (DeviceInfo, error) // TotalBytes, FreeBytes

// ios/crashreport/crashreport.go
func ListReports(device ios.DeviceEntry, pattern string) ([]string, error)
func DownloadReports(device ios.DeviceEntry, pattern, targetdir string) error
func RemoveReports(device ios.DeviceEntry, cwd, pattern string) error

// ios/house_arrest/house_arrest.go
func New(device ios.DeviceEntry, bundleID string) (*afc.Client, error)
// Sends {"Command":"VendContainer","Identifier":bundleID}. No VendDocuments.

// ios/installationproxy/installationproxy.go
func New(device ios.DeviceEntry) (*Connection, error)
func (c *Connection) BrowseUserApps() ([]AppInfo, error)
func (c *Connection) BrowseSystemApps() ([]AppInfo, error)
func (c *Connection) BrowseFileSharingApps() ([]AppInfo, error)
func (c *Connection) Uninstall(bundleId string) error
// AppInfo is map[string]any → DynamicDiskUsage, StaticDiskUsage, Container,
// Path, UIFileSharingEnabled all accessible via map index.
```

AFC opcodes include `removePath = 0x08` and `removePathAndContents = 0x22`;
`RemoveAll` uses the recursive opcode.

### Python signatures (verbatim from source at the pinned SHA)

```python
# services/crash_reports.py
async def clear(self, path: str = "/") -> None
async def ls(self, path: str = "/", depth: int = 1) -> list[str]
async def pull(self, out, entry="/", erase=False, match=None, progress_bar=True)
async def flush(self) -> None
async def get_new_sysdiagnose(self, out, erase=True, *, timeout=None, callback=None)

# services/afc.py
async def listdir(self, filename: str)
async def walk(self, dirname: str)
async def stat(self, filename: str)
async def rm_single(self, filename: str, force: bool = False) -> bool
async def rm(self, filename: str, match=None, force: bool = False) -> list[str]
async def pull(self, relative_src, dst, match=None, ...)
async def push(self, local_path, remote_path, callback=None)
async def get_device_info(self) -> dict   # FSTotalBytes, FSFreeBytes

# services/house_arrest.py
VEND_CONTAINER = "VendContainer"
VEND_DOCUMENTS = "VendDocuments"
@classmethod
async def create(cls, lockdown, bundle_id, documents_only: bool = False)

# services/installation_proxy.py
async def get_apps(self, application_type="Any", calculate_sizes=False,
                   bundle_identifiers=None, show_placeholders=False) -> dict[str, dict]
def uninstall(self, bundle_identifier, options, handler, *args)
```

---

## 3. The `VendContainer` disagreement — the central capability question

The two research agents reached **different conclusions** on whether
`house_arrest`'s `VendContainer` actually returns the sandbox for an arbitrary
App Store app on a stock iOS device. The disagreement matters because nearly
every "clean per-app caches" feature depends on it.

- The Go agent inspected go-ios source and concluded `VendContainer` "works for
  any installed third-party app, regardless of `UIFileSharingEnabled`", with a
  caveat that "the daemon decides".
- The Python agent inspected pymobiledevice3 source and was more pessimistic:
  `VendContainer` "works for very few user apps; `VendDocuments` works for
  apps with file-sharing".

Both readings of the *client* code are correct — both libraries simply forward
the verb. The disagreement is about what Apple's on-device
`mobile_house_arrest` daemon will actually honour. Apple's policy here is
undocumented and has tightened over iOS versions. The realistic picture, taking
the pessimistic view (which neither library can verify from its own source):

- **`VendDocuments`** — succeeds for apps whose Info.plist sets
  `UIFileSharingEnabled = YES` (a small subset of apps: VLC, Procreate, some
  document editors). Returns AFC rooted at the app's `Documents/`.
- **`VendContainer`** — historically succeeded for any user app; on recent iOS
  (17+) it is **commonly refused** for App Store apps unless the binary is
  signed with `get-task-allow` (TestFlight, Xcode-installed, sideloaded). For
  vanilla App Store apps the daemon typically returns an error and
  `house_arrest.New` fails.
- **Open go-ios issue #653** corroborates this: `ios fsync` with `--app=`
  fails with `"connect afc service failed"` on iOS 17.1 for certain bundles.

**Implication for `ios-tidy`**: do not promise blanket "clean any app's caches".
The honest model is *probe each app with `house_arrest.New`, surface which
apps the device actually vends, and offer cleanup only on those*. Apps that
refuse vending get listed with their reported `DynamicDiskUsage` and a note
that the user would have to use Settings → General → iPhone Storage to
manage them.

This must be verified on a real iOS 17/18 device before any UI promises
cleanup. (Open question, §7.)

---

## 4. Realistic deletion surface

| Target | Typical size | Safe? | Mechanism | Caveats |
|---|---|---|---|---|
| Crash logs | 100 MB – 2 GB | Yes (Apple regenerates the dir) | `crashreport.RemoveReports` / `CrashReportsManager.clear` | Must `flush()` mover service first to capture pending crashes. Files only — no rmdir. |
| Sysdiagnose archives | 100s of MB each | Yes (host can fetch first) | `CrashReportsManager.get_new_sysdiagnose(erase=True)` (Python). Go has no high-level equivalent — would need direct AFC walk on the sysdiagnose subdir. | Only generated by Side+VolUp gesture or `sysdiagnose` daemon. |
| `/PublicStaging` | usually empty; sometimes GBs of stuck IPAs | Yes | AFC `RemoveAll` | Used by installers; clean only when no install is in progress. |
| Per-app Documents/tmp/Caches (file-sharing apps) | varies | Documents = user data (warn); tmp / Library/Caches = generally safe | `house_arrest` + `VendDocuments` (Python only) or `VendContainer` (both, often fails) | See §3. tmp regenerates; Caches will be rebuilt on next app launch. |
| Per-app sandbox (sideloaded / TestFlight / dev-signed apps) | varies | Same as above | `house_arrest` + `VendContainer` | Works reliably only when the app has `get-task-allow`. |
| App uninstall (full reclaim including binary + container) | bundle 10s of MB + data | Destructive; loses settings | `installation_proxy.Uninstall(bundleID)` | Definitive but coarse — no "offload, keep data" equivalent on the public surface. |
| Free / total volume bytes (for UI) | — | read-only | AFC `DeviceInfo` (`TotalBytes` / `FreeBytes`) | Best available without lockdown queries; small skew from Settings expected. |
| Per-app size breakdown | — | read-only | `installation_proxy.BrowseUserApps()` → `DynamicDiskUsage` / `StaticDiskUsage` | Sometimes only populated after app has been launched recently. |
| `/var/mobile/Media/DCIM` (Photos) | very large | **No** — deleting via AFC bypasses Photos.app and corrupts the Photos.sqlite DB | AFC reaches it; **do not offer delete** | Inspect-only. Use Photos app or PhotoKit on the host for actual deletion. |
| `/var/mobile/Media/Recordings`, `/Books`, `/Downloads` | small | Mixed — same DB-orphan risk for Books and Voice Memos | AFC | Inspect-only by default; warn loudly if exposed. |
| Provisioning profiles | < 1 MB each | Yes | `misagent.Remove` | Trivial impact — not worth a top-level command. |
| App Group containers (iOS 17+, Developer Mode + DDI + tunneld) | varies | Read-only (no delete verb in CoreDevice FileService) | Python only, gated on tunneld | Inspect-only. Source as of pinned SHA has no `Delete` verb. |

---

## 5. Impossible without jailbreak (both languages, confirmed)

1. **System caches outside any app sandbox** (`/private/var/mobile/Library/Caches/com.apple.*`) — no service exposes them; AFC view is limited to `/var/mobile/Media`, the crash log dir, and vended app sandboxes.
2. **Safari / WebKit caches** — owned by system app sandboxes the daemon will not vend.
3. **Mail attachments** — owned by `com.apple.mobilemail`, same story.
4. **"Other" / "System Data" storage bucket** — Apple does not decompose this even on-device beyond Settings' opaque label.
5. **App offloading** — `installation_proxy` exposes `Install` / `Upgrade` / `Uninstall` only. The "keep data, drop binary" path is iCloud-mediated and has no public service.
6. **Per-app Caches deletion for App Store apps that the daemon refuses to vend** — see §3.
7. **iCloud control** (Optimize Photos, iCloud Drive offload).
8. **Music / Podcasts downloaded media** — files reachable via AFC but tied to a CoreData DB that AFC can't update; deletion creates orphans.

---

## 6. Pairing & trust

- **usbmuxd** is a stock macOS LaunchDaemon (`/Library/Apple/System/Library/LaunchDaemons/com.apple.usbmuxd.plist`). Always running. Both libraries connect to its UNIX socket at `/var/run/usbmuxd`; neither needs Homebrew `libimobiledevice`/`usbmuxd`.
- **First-run trust flow**: device must be plugged in and the user must tap "Trust this computer" on the device itself. Neither library can trigger that prompt remotely except by attempting a Pair (which is what triggers the dialog). While the prompt is open, lockdown returns `PairingDialogResponsePending`; on user denial, `UserDeniedPairingError`.
- **Trust state detection in Go**: no first-class `IsPaired()`. The idiom is to attempt `ios.Pair()` or a lockdown `StartSession()` and interpret the response.
- **Trust state detection in Python**: `LockdownClient.paired: bool` after construction.
- **Open go-ios issue #710 (macOS 26 Tahoe)**: pair-record reads blocked by TCC; the documented `--pair-record-path` workaround is reported as not working. As of 2026-05-23, no fix. Risk for users on Tahoe; macOS 14/15 fine.

---

## 7. Open questions (need a real device to settle)

1. **Which App Store apps does `mobile_house_arrest` actually vend on iOS 17/18?** Source can't answer this. Settles by enumerating `BrowseUserApps` and attempting `house_arrest.New` against each.
2. **Does `crashreport.RemoveReports` complete cleanly on iOS 17+ for a directory with thousands of entries?** Some rate-limiting suspected.
3. **Does `afc.DeviceInfo.FreeBytes` match Settings → About → Available within a few hundred MB on current iOS?** Historic small skew.
4. **Is `DynamicDiskUsage` populated for cold apps on current iOS?** Or only after recent launch.
5. **Reproducibility of go-ios #653 (`connect afc service failed` on iOS 17.1+)** on this particular phone.
6. **macOS Tahoe TCC #710**: does it affect read-only operations too, or only writes that need the pair-record path?

These are the integration-test items.

---

## 8. Maturity & risks

| Concern | Go (go-ios) | Python (pymobiledevice3) |
|---|---|---|
| Activity | 8 releases / 6 weeks | weekly + same-day patch releases |
| Stars | 2.1k | 2.3k |
| Open issues | 171 | 97 |
| License | MIT | GPL-3.0-or-later |
| iOS 17/18 tunneld | Thin RSD support; works for lockdown-era services without tunnel | Mature; some open flakiness (issues #1330, #1378, #1682) |
| Top known risk | #710 Tahoe TCC pair-record block; #653 house_arrest iOS 17.1+ | tunneld root requirement; CoreDevice FileService is read-only |
| Distribution | static binary, ~5–10 MB | PyInstaller bundle 100–200 MB, slow cold start |
| Mockability for tests | seams need wrapping (concrete structs, no exported interfaces) — but no global state to fight | services accept an injected `LockdownServiceProvider`; `AsyncMock` over `send_plist` / `recv_plist` covers most wrapping |

---

## 9. Language choice — recommendation

**Recommend Go.** Reasoning:

1. The ceiling on storage cleanup is set by Apple's daemon, not the host library. Both languages hit the same ceiling. Python's larger surface (CoreDevice, tunneld) does not translate to additional *deletion* capability — CoreDevice FileService is read-only in pmd3's current source.
2. GPL-3.0 on pymobiledevice3 is a real constraint. If `ios-tidy` is ever shared (publicly or with colleagues), it would have to inherit GPL-3 — fine for a personal tool, sticky for anything else. go-ios is MIT.
3. Distribution: a sub-10 MB notarised Go binary is dramatically nicer to install than a 100–200 MB PyInstaller bundle.
4. Aligns with `development_rule`'s explicit Go-for-CLIs default (`knowledge/philosophy/technology-selection.md`: "Use [Go] when: ... CLI tools").
5. The known Go risks (#710 Tahoe, #653 iOS 17.1) are real but not language-fixable — they live in the daemon / host OS, not the library.

The case for Python remains real for *future* features outside this scope —
debugger attach, instruments-class profiling, CoreDevice for iOS 17+
inspection. If that becomes interesting later, a Python sidecar can be added
without rewriting the storage-cleanup CLI.

---

## 10. Headline

A non-jailbroken iPhone exposes a small, well-defined deletion surface to a
host Mac: crash logs (yes, big win), sysdiagnose archives (yes), the per-app
sandbox of *apps the device chooses to vend* (highly variable — see §3), and
full app uninstall. Everything else widely cited online as "iPhone cleaner"
features — system caches, Safari caches, Mail attachments, "Other" storage,
selective app offload — is gated by the iOS sandbox and not reachable in any
language. The honest tool is therefore a focused crash-log cleaner + a probing
per-app browser that tells the user exactly which apps it can and cannot touch
+ an inspection view of disk usage. Anything more ambitious would be a lie.
