# AGENTS.md — welock-desktop

Cross-platform (macOS + Windows) **desktop client** for WeLock / AireKey smart locks.
Built with [Fyne](https://fyne.io) v2.8 (pure-Go widgets) and native Bluetooth
(`tinygo.org/x/bluetooth` → CoreBluetooth/WinRT, behind the `tinygobt` build tag).
Repo: `github.com/thibauddavid/welock-desktop`.

## Architecture — the app is a pure UI + BLE radio

All protocol/business logic (signing, endpoints, BLE frame codec, nonce→mint mapping,
view-model shaping, PIN validation) lives in the shared **engine**. This repo
imports **nothing** from the engine: it ships as a compiled, **opaque helper
binary** the app embeds and spawns, and the app talks to it over line-delimited JSON-RPC.
This is the native analog of a web client loading a compiled, prebuilt wasm module — the engine
stays private while this repo is public and self-contained.

```
internal/ui (Fyne)  ──►  internal/app.Core  ──JSON-RPC over stdio──►  engine helper
                                            └──►  ble_radio.go (tinygo)  ── GATT only ──┘
   view models: internal/app/viewmodels.go (local mirrors of the helper's JSON replies)
```

- **`internal/app/sidecar`** — spawns the helper (embedded binary, extracted to the user
  cache; or `WELOCK_SIDECAR=<path>` in dev) and multiplexes JSON-RPC calls to it. The
  committed per-platform binaries under `internal/app/sidecar/bin/` ARE the engine.
- **`internal/app/core.go`** — `Core`: one typed method per feature, each an RPC call. The
  app stays the **token owner** (every RPC response echoes the helper's rotated tokens;
  `store.go` persists them). Constant enums are cached at `New()`; pure helpers
  (`ValidatePin`, `ValidityWindow`, …) run off the network lock so the UI's synchronous
  form builders stay instant.
- **`internal/app/viewmodels.go`** — local Go structs the helper's JSON decodes into (the
  Go analog of a web client's `api/types.ts`). Never reach into the engine for a type.
- **BLE** (`ble.go` / `ble_radio.go`, `//go:build tinygobt`) — the app keeps the radio as a
  **pure GATT pipe** (scan/connect/write/notify). Frame encode/decode + the cloud mint are
  helper RPCs; unlock is unidirectional: read nonce → RPC mint → write → RPC report. The
  default build (`ble_stub.go`) has no radio.

**The rule:** never re-implement a WeLock rule here — add an RPC method to the helper
(the engine's `cmd/sidecar`) and call it. The RPC method list is defined in `core.go`.

## Build / run

Self-contained — no private access, siblings, or tokens needed (the engine binary is
committed):

```bash
go run  -tags tinygobt .          # real radio (BLE works)
go run  .                         # BLE stubbed — UI-only dev
go build -tags tinygobt ./...     # native BLE + Fyne
go build ./...                    # stubbed BLE + Fyne
go vet ./... ; gofmt -l . ; go test ./...
```

Package (per-OS, needs that OS): `fyne package -tags tinygobt` → `WeLock.app` / `WeLock.exe`.
macOS needs `NSBluetoothAlwaysUsageDescription` in the bundle Info.plist or CoreBluetooth is
silently denied. Fyne + tinygo-bluetooth need CGO → build each OS on its own runner
(`.github/workflows/release.yml`). The helper binary itself has no cgo.

> **go.mod caveat:** `tinygo.org/x/bluetooth` is only referenced under the `tinygobt` tag,
> which `go mod tidy` does NOT evaluate — a tidy will try to drop it. If it disappears,
> re-add with `go get tinygo.org/x/bluetooth@v0.15.0`.

## Regenerating the engine

On an engine bump, rebuild the committed helper binaries (the one step needing the
engine source) and commit them — mirrors a web client re-fetching a prebuilt wasm module:

```bash
tools/build-sidecar.sh <path-to-engine-src>   # or set WELOCK_ENGINE_SRC
```

It cross-builds a universal (arm64+amd64) macOS binary and a Windows amd64 binary into
`internal/app/sidecar/bin/`.

## Reply-shape gotchas (the helper returns exactly what mobile.Session did)

The helper's `result` is the raw JSON — or bare string — the core would have returned, so
`core.go` decodes each the same way. Watch for:

- **Bare-string returns:** `remoteUnlock`, `feibiLockOpen`, all `ufun*` return a bare command
  id (not JSON) — poll `commandStatus`. `addTempPassword` returns the bare PIN,
  `nextCredentialNumber` a bare number (both may arrive JSON-quoted — `unquote`).
- **`devices` is a RAW mixed list** (locks + gateways, `type` 6 = gateway) → decode into the
  permissive `DeviceListItem`; list gateways from `gateways`.
- **Mint JSON** is `{"bytes":[]int,"serverID":"..."}` → convert `[]int`→`[]byte` (`MintResult.Command`).
- **Empty is not an error:** the "no data" case comes back as `""`/`"[]"`, treated as a no-op.
  An error may carry code **1000** = token expired/invalidated (single-session) → re-login;
  detect via `IsAuthError` (a `*sidecar.RPCError` with `Code==1000`).
- **Persist tokens after every call:** the helper auto-refreshes; `Core.rpc` reads the echoed
  tokens and saves. Pure helpers bypass this (they never rotate tokens).
- **Fyne threading:** every RPC/BLE call BLOCKS. Run it on a goroutine (`runAsync`); mutate
  widgets ONLY via `fyne.Do(...)`. Getting this wrong = frozen UI or data races.

## Layout

```
main.go                 entry: build Core (spawns helper), load session, show Login or Main
internal/app/           core.go (Core: RPC methods, token owner), viewmodels.go (mirror types),
                        store.go (session.json in os.UserConfigDir/WeLock), prefs.go (prefs.json:
                        install id + analytics opt-out, survives logout), models.go (local
                        shapes), ble.go / ble_radio.go (//go:build tinygobt), ble_stub.go,
                        sidecar/ (JSON-RPC client + embed/extract/spawn; bin/ = engine binaries)
internal/analytics/     Amplitude HTTP client (anonymous product telemetry; see rule below)
internal/ui/            login, main_window, device_detail, bluetooth_tab, remote_tab,
                        manage_tab, dialogs, widgets (runAsync), settings, theme, kit
tools/                  build-sidecar.sh (regenerate engine), screenshots/ (README assets)
.github/workflows/release.yml   macOS+Windows matrix: fyne package -tags tinygobt (no token)
```

**Analytics rule:** telemetry is a client concern — it lives ONLY here (`internal/analytics`),
never in the engine/sidecar. It sends anonymous aggregate events (random install id, app
version, OS, coarse `Core.Track(event, props)` calls) and MUST NEVER carry account, phone,
tokens, device/lock ids, or credential values — `props` are low-cardinality labels only
(`{"transport":"ble"}`). The Amplitude key is injected into official releases via
`-ldflags -X …analytics.apiKey=` (a CI secret); source/dev/fork builds have no key and send
nothing. Opt-out is on by default in releases (Settings → Privacy); the choice persists in
`prefs.json`.
