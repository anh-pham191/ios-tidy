# Acceptance walkthrough — ios-tidy M6

Date: <fill in YYYY-MM-DD>
Device: <model, iOS version, UDID>
Host: <macOS version>

## Steps

1. `ios-tidy devices` — expected: table with the device, trust state `trusted`. Result:
2. `ios-tidy storage` — expected: header line + per-app table sorted desc. Result:
3. `ios-tidy storage --json | jq '.device'` — expected: well-formed JSON. Result:
4. `ios-tidy crashlogs list` — expected: list with sizes + mtimes. Result:
5. `ios-tidy crashlogs pull --out /tmp/crashlogs-test --pattern '*.ips'` — expected: files copied. Result:
6. `ios-tidy crashlogs clean --dry-run` — expected: plan + "Dry run — no changes made.", no deletion. Result:
7. `ios-tidy crashlogs clean` — answer `n` — expected: aborted, no deletion. Result:
8. `ios-tidy crashlogs clean` — answer `y` — expected: summary. Result:
9. `ios-tidy apps list --limit 10` — expected: top-10 apps by total bytes. Result:
10. `ios-tidy apps probe --all --timeout 10s` — expected: outcome column populated. Result:
11. Pick a `vended` bundle ID. `ios-tidy apps clean <bundle> --dry-run` — expected: plan, no deletion. Result:
12. `ios-tidy apps clean <bundle>` — answer `n` — expected: aborted. Result:
13. `ios-tidy apps clean <bundle>` — answer `y` — expected: deletion summary. Result:
14. `ios-tidy apps clean <bundle> --include-documents` — at the typed-bundle prompt, type a wrong value — expected: `Bundle ID did not match. Aborted.`, no deletion. Result:
15. `ios-tidy apps clean <bundle> --include-documents` — type the correct bundle — expected: deletion summary. (Only run this on a sentinel/test app whose Documents data you can lose.) Result:
16. Pick a `refused` bundle ID. `ios-tidy apps clean <bundle>` — expected: probe-gate refusal pointing at `apps probe`. Result:
17. Stage stale probe: edit the probe JSON to mark a known-refused bundle as vended, then `ios-tidy apps clean <bundle>` — expected: open-fails, error mentions "may be stale" and points at `apps probe`. Restore the file when done. Result:

## Notes

<free text>
