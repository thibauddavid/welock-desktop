package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/thibauddavid/welock-desktop/internal/analytics"
	"github.com/thibauddavid/welock-desktop/internal/app/sidecar"
)

// Core is the single backend surface the UI talks to. It drives the engine helper
// process over JSON-RPC (internal/app/sidecar), serialises every cloud call behind a
// mutex, and persists rotated tokens after each request (the helper's auto-refresh may
// rotate them mid-request). Every rule comes from the helper — Core adds no protocol
// logic of its own, and this repo imports NOTHING from the engine.
type Core struct {
	mu     sync.Mutex
	client *sidecar.Client
	store  *store

	// deviceID is the immutable device id the session is pinned to. It never changes
	// after New(), so it is cached here and served without taking mu.
	deviceID string
	// loggedIn mirrors whether an access token is currently held, as an atomic so
	// LoggedIn() never contends on mu.
	loggedIn atomic.Bool

	// lastAccess/lastRefresh track what we last wrote to disk so we only Save on change.
	lastAccess  string
	lastRefresh string

	// validityPresets/credentialTypes are the helper's constant enums, fetched once at
	// New() so the UI's synchronous form builders read them instantly (never over the pipe).
	validityPresets []ValidityPreset
	credentialTypes []CredentialType

	// analytics is the anonymous product-telemetry client (nil-safe). prefsStore/prefs
	// persist the install id + opt-out across runs (independent of the login session).
	analytics  *analytics.Client
	prefsStore *prefsStore
	prefs      prefs
}

// New builds a Core: it loads any persisted session, spawns the helper pinned to the
// persisted (or freshly generated) device id and stored tokens, and caches the helper's
// constant enums. baseURL "" targets the public cloud.
func New(baseURL string) (*Core, error) {
	st, err := newStore()
	if err != nil {
		return nil, err
	}
	saved, err := st.Load()
	if err != nil {
		return nil, err
	}

	client, err := sidecar.Start(baseURL, saved.DeviceID, saved.AccessToken, saved.RefreshToken)
	if err != nil {
		return nil, err
	}

	c := &Core{
		client:      client,
		store:       st,
		deviceID:    client.DeviceID(),
		lastAccess:  client.AccessToken(),
		lastRefresh: client.RefreshToken(),
	}
	c.loggedIn.Store(client.AccessToken() != "")
	// A freshly generated device id must be persisted so it is stable across runs.
	if saved.DeviceID == "" {
		_ = c.saveSession()
	}
	if err := c.loadConstants(); err != nil {
		_ = client.Close()
		return nil, err
	}
	// WELOCK_VERBOSE=1 turns on the helper's stderr request/response log (visible when the
	// app is launched from a terminal). Also toggleable at runtime via SetVerbose.
	if v := os.Getenv("WELOCK_VERBOSE"); v == "1" || v == "true" {
		c.SetVerbose(true)
	}
	c.initAnalytics()
	return c, nil
}

// Close shuts down the helper process. The UI calls it on quit/logout.
func (c *Core) Close() error { return c.client.Close() }

// --- analytics ------------------------------------------------------------

// initAnalytics loads device-local prefs (generating a stable anonymous install id on
// first run) and builds the telemetry client. Best-effort: on any error analytics stays
// nil and every Track becomes a no-op, leaving the app unaffected.
func (c *Core) initAnalytics() {
	ps, err := newPrefsStore()
	if err != nil {
		return
	}
	pr, err := ps.Load()
	if err != nil {
		return
	}
	if pr.InstallID == "" {
		pr.InstallID = newInstallID()
		_ = ps.Save(pr)
	}
	c.prefsStore = ps
	c.prefs = pr
	// Opt-out model: enabled unless the user has explicitly disabled it (and a key exists).
	c.analytics = analytics.New(pr.InstallID, !pr.AnalyticsDisabled)
}

// Track sends one anonymous analytics event (no-op if disabled or no key is baked in).
// props must carry only non-identifying, low-cardinality values — never account, lock,
// token, or credential data.
func (c *Core) Track(event string, props map[string]any) {
	if c.analytics != nil {
		c.analytics.Track(event, props)
	}
}

// AnalyticsAvailable reports whether this build carries an analytics key (so the UI can
// decide whether to show the opt-out toggle at all).
func (c *Core) AnalyticsAvailable() bool { return c.analytics != nil && c.analytics.Available() }

// AnalyticsEnabled reports whether analytics is currently on.
func (c *Core) AnalyticsEnabled() bool { return c.analytics != nil && c.analytics.Enabled() }

// SetAnalyticsEnabled toggles analytics and persists the choice across runs.
func (c *Core) SetAnalyticsEnabled(on bool) {
	if c.analytics == nil {
		return
	}
	c.analytics.SetEnabled(on)
	if c.prefsStore != nil {
		c.prefs.AnalyticsDisabled = !on
		_ = c.prefsStore.Save(c.prefs)
	}
}

// SetAppVersion records the packaged app version reported with analytics events.
func (c *Core) SetAppVersion(v string) {
	if c.analytics != nil {
		c.analytics.SetVersion(v)
	}
}

// SetVerbose toggles the helper's wire-level request/response logging to stderr.
func (c *Core) SetVerbose(on bool) { _, _ = c.client.Call("setVerbose", on) }

// loadConstants caches the helper's constant enums (also a startup health check — these
// are session-less and can only fail if the helper is broken).
func (c *Core) loadConstants() error {
	out, err := c.client.Call("validityPresets")
	if err != nil {
		return err
	}
	if err := decode(out, &c.validityPresets); err != nil {
		return err
	}
	out, err = c.client.Call("credentialTypes")
	if err != nil {
		return err
	}
	return decode(out, &c.credentialTypes)
}

// --- session helpers ------------------------------------------------------

// saveSession writes the current device id + tokens to disk unconditionally.
func (c *Core) saveSession() error {
	c.lastAccess = c.client.AccessToken()
	c.lastRefresh = c.client.RefreshToken()
	return c.store.Save(StoredSession{
		DeviceID:     c.deviceID,
		AccessToken:  c.lastAccess,
		RefreshToken: c.lastRefresh,
	})
}

// persistTokens saves the token pair only if it changed since the last write. It is
// called after every request because the helper's auto-refresh may have rotated them.
func (c *Core) persistTokens() {
	a := c.client.AccessToken()
	r := c.client.RefreshToken()
	if a == c.lastAccess && r == c.lastRefresh {
		return
	}
	_ = c.saveSession()
}

// rpc runs a JSON-returning helper method under the lock and persists any rotated tokens
// afterwards. ctx cancellation is honoured before dispatch.
func (c *Core) rpc(ctx context.Context, method string, args ...any) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	out, err := c.client.Call(method, args...)
	c.persistTokens()
	return out, err
}

// rpcErr runs an error-only helper method (discarding the result string).
func (c *Core) rpcErr(ctx context.Context, method string, args ...any) error {
	_, err := c.rpc(ctx, method, args...)
	return err
}

// pureCall invokes a session-less pure helper directly (no mu, no token persistence), so
// it stays instant even while a cloud call holds mu. Errors are swallowed (these helpers
// are deterministic and local; a transient failure returns the zero value).
func (c *Core) pureCall(method string, args ...any) string {
	out, err := c.client.Call(method, args...)
	if err != nil {
		return ""
	}
	return out
}

// decode unmarshals a JSON reply into v, treating an empty/whitespace body (the "no data"
// case, code 2000) as a no-op rather than an error.
func decode[T any](s string, v *T) error {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	return json.Unmarshal([]byte(s), v)
}

// --- state / helpers ------------------------------------------------------

// LoggedIn reports whether an access token is currently held (atomic mirror, no mu).
func (c *Core) LoggedIn() bool { return c.loggedIn.Load() }

// DeviceID returns the stable device id this session is pinned to (cached, no mu).
func (c *Core) DeviceID() string { return c.deviceID }

// Logout clears the in-memory tokens and wipes the persisted session.
func (c *Core) Logout() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.client.SetTokens("", "")
	c.lastAccess = ""
	c.lastRefresh = ""
	c.loggedIn.Store(false)
	return c.store.Clear()
}

// IsAuthError reports whether err is the single-session token-expired error (helper
// RPCError with Code 1000) — the signal to prompt a re-login.
func IsAuthError(err error) bool {
	var re *sidecar.RPCError
	if errors.As(err, &re) {
		return re.Code == 1000
	}
	return false
}

// --- validity / pin helpers (pure, instant) -------------------------------

// ValidityPresets returns the selectable validity windows (cached at New()).
func (c *Core) ValidityPresets() []ValidityPreset { return c.validityPresets }

// CredentialTypes returns the canonical credential-kind list (cached at New()).
func (c *Core) CredentialTypes() []CredentialType { return c.credentialTypes }

// ValidityWindow resolves a preset key into a [start,end] unix-second window.
func (c *Core) ValidityWindow(key string, now int64) (start, end int64) {
	var w struct {
		StartTime int64 `json:"startTime"`
		EndTime   int64 `json:"endTime"`
	}
	_ = decode(c.pureCall("validityWindow", key, now), &w)
	return w.StartTime, w.EndTime
}

// ValidatePin returns "" if pin is valid for deviceModel, else a reason string.
func (c *Core) ValidatePin(deviceModel, pin string) string {
	return c.pureCall("validatePin", deviceModel, pin)
}

// GroupCredentialsByOwner groups a raw credentials JSON array by owning user.
func (c *Core) GroupCredentialsByOwner(credsJSON json.RawMessage) []OwnerGroup {
	var groups []OwnerGroup
	_ = decode(c.pureCall("groupCredentialsByOwner", string(credsJSON)), &groups)
	return groups
}

// --- auth -----------------------------------------------------------------

// StartWhatsAppLogin requests a WhatsApp login challenge (code + prefilled message URL).
func (c *Core) StartWhatsAppLogin(ctx context.Context) (*WhatsAppLogin, error) {
	out, err := c.rpc(ctx, "startWhatsAppLogin")
	if err != nil {
		return nil, err
	}
	var wl WhatsAppLogin
	if err := decode(out, &wl); err != nil {
		return nil, err
	}
	return &wl, nil
}

// PollWhatsAppLogin checks once whether the WhatsApp message has bound. It returns
// (tokens, nil) on success, (nil, nil) while still pending, or (nil, err).
func (c *Core) PollWhatsAppLogin(ctx context.Context, code string) (*Tokens, error) {
	out, err := c.rpc(ctx, "pollWhatsAppLogin", code)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(out) == "" {
		return nil, nil // pending
	}
	return c.applyTokens(out)
}

// Login signs in with an account (email/phone) + password.
func (c *Core) Login(ctx context.Context, account, password string) (*Tokens, error) {
	out, err := c.rpc(ctx, "login", account, password)
	if err != nil {
		return nil, err
	}
	return c.applyTokens(out)
}

// LoginWithToken signs in by importing an existing access token (and an optional refresh
// token) directly, then validates it with a lightweight authed call. On an auth rejection
// the imported tokens are rolled back so the app stays logged out.
func (c *Core) LoginWithToken(ctx context.Context, accessToken, refreshToken string) error {
	accessToken = strings.TrimSpace(accessToken)
	refreshToken = strings.TrimSpace(refreshToken)
	if accessToken == "" {
		return errors.New("an access token is required")
	}
	c.mu.Lock()
	_ = c.client.SetTokens(accessToken, refreshToken)
	_ = c.saveSession()
	c.mu.Unlock()
	c.loggedIn.Store(true)

	// Validate the token with a cheap authed request; roll back if it is rejected.
	if _, err := c.Devices(ctx); err != nil {
		if IsAuthError(err) {
			_ = c.Logout()
			return errors.New("token rejected — it may be expired or invalid")
		}
		return err
	}
	return nil
}

// Refresh trades the stored refresh token for a fresh access token.
func (c *Core) Refresh(ctx context.Context) (*Tokens, error) {
	out, err := c.rpc(ctx, "refresh")
	if err != nil {
		return nil, err
	}
	return c.applyTokens(out)
}

// applyTokens decodes a tokens JSON reply and persists it.
func (c *Core) applyTokens(out string) (*Tokens, error) {
	var t Tokens
	if err := decode(out, &t); err != nil {
		return nil, err
	}
	c.mu.Lock()
	_ = c.saveSession()
	c.mu.Unlock()
	c.loggedIn.Store(c.client.AccessToken() != "")
	return &t, nil
}

// --- devices --------------------------------------------------------------

// Devices lists the account's devices as the RAW permissive rows (locks AND gateways
// mixed; type 6 rows are gateways).
func (c *Core) Devices(ctx context.Context) ([]DeviceListItem, error) {
	out, err := c.rpc(ctx, "devices")
	if err != nil {
		return nil, err
	}
	var items []DeviceListItem
	if err := decode(out, &items); err != nil {
		return nil, err
	}
	return items, nil
}

// DeviceInfo returns the shaped device view-model (with battery + capabilities).
func (c *Core) DeviceInfo(ctx context.Context, deviceNumber string) (*Device, error) {
	out, err := c.rpc(ctx, "deviceInfo", deviceNumber)
	if err != nil {
		return nil, err
	}
	var d Device
	if err := decode(out, &d); err != nil {
		return nil, err
	}
	return &d, nil
}

// UnlockRecords returns a page of the device's unlock history.
func (c *Core) UnlockRecords(ctx context.Context, deviceNumber string, pageNo, pageSize int) ([]UnlockRecord, error) {
	out, err := c.rpc(ctx, "unlockRecords", deviceNumber, pageNo, pageSize)
	if err != nil {
		return nil, err
	}
	var recs []UnlockRecord
	if err := decode(out, &recs); err != nil {
		return nil, err
	}
	return recs, nil
}

// RenameDevice sets a device's display name.
func (c *Core) RenameDevice(ctx context.Context, deviceNumber, deviceName, value string) error {
	return c.rpcErr(ctx, "renameDevice", deviceNumber, deviceName, value)
}

// BindDevice binds a scanned/QR device to the account. Returns the raw shaped JSON reply.
func (c *Core) BindDevice(ctx context.Context, deviceNumber, qrStr, deviceType, deviceModel string, longitude, latitude float64, serverID string) (string, error) {
	return c.rpc(ctx, "bindDevice", deviceNumber, qrStr, deviceType, deviceModel, longitude, latitude, serverID)
}

// DeleteDevice unbinds a device from the account.
func (c *Core) DeleteDevice(ctx context.Context, deviceNumber, deviceName string) error {
	return c.rpcErr(ctx, "deleteDevice", deviceNumber, deviceName)
}

// ActivationBind binds an activation code. Returns the raw shaped JSON reply.
func (c *Core) ActivationBind(ctx context.Context, code string, timeOffset int) (string, error) {
	return c.rpc(ctx, "activationBind", code, timeOffset)
}

// ActivationList lists activations filtered by status. Returns the raw shaped JSON reply.
func (c *Core) ActivationList(ctx context.Context, status int) (string, error) {
	return c.rpc(ctx, "activationList", status)
}

// --- temp passwords -------------------------------------------------------

// GatewayCredentials returns every keypad PIN on the lock as the unified list the core
// shapes (bff.MergeKeypadPins) — permanent member PINs + time-limited gateway PINs, each
// tagged with how it deletes. The merge lives in the core; this just decodes it.
func (c *Core) GatewayCredentials(ctx context.Context, deviceNumber, deviceName string) ([]PinEntry, error) {
	out, err := c.rpc(ctx, "keypadPins", deviceNumber, deviceName)
	if err != nil {
		return nil, err
	}
	var pins []PinEntry
	if err := decode(out, &pins); err != nil {
		return nil, err
	}
	return pins, nil
}

// TempPasswords lists the device's temporary passwords.
func (c *Core) TempPasswords(ctx context.Context, deviceNumber, deviceName string) ([]TempPassword, error) {
	out, err := c.rpc(ctx, "tempPasswords", deviceNumber, deviceName)
	if err != nil {
		return nil, err
	}
	var tps []TempPassword
	if err := decode(out, &tps); err != nil {
		return nil, err
	}
	return tps, nil
}

// AddTempPassword creates a temporary password and returns the bare PIN.
func (c *Core) AddTempPassword(ctx context.Context, deviceNumber, deviceName string, startTime, endTime int64, remark string, typ int) (string, error) {
	out, err := c.rpc(ctx, "addTempPassword", deviceNumber, deviceName, startTime, endTime, remark, typ)
	if err != nil {
		return "", err
	}
	return unquote(out), nil
}

// DeleteTempPassword removes a temporary password by id.
func (c *Core) DeleteTempPassword(ctx context.Context, deviceNumber, deviceName, id string) error {
	return c.rpcErr(ctx, "deleteTempPassword", deviceNumber, deviceName, id)
}

// unquote turns a possibly-JSON-quoted bare string into its plain value.
func unquote(s string) string {
	s = strings.TrimSpace(s)
	var v string
	if err := json.Unmarshal([]byte(s), &v); err == nil {
		return v
	}
	return s
}

// --- lock commands --------------------------------------------------------

// RemoteUnlock triggers a gateway-relayed unlock and returns the BARE command id.
func (c *Core) RemoteUnlock(ctx context.Context, deviceNumber, deviceName string) (string, error) {
	out, err := c.rpc(ctx, "remoteUnlock", deviceNumber, deviceName)
	return strings.TrimSpace(out), err
}

// FeibiLockOpen triggers a Feibi remote unlock and returns the BARE command id.
func (c *Core) FeibiLockOpen(ctx context.Context, deviceNumber, deviceName string) (string, error) {
	out, err := c.rpc(ctx, "feibiLockOpen", deviceNumber, deviceName)
	return strings.TrimSpace(out), err
}

// UnlockCommand mints the BLE unlock frame for a locally-read nonce.
func (c *Core) UnlockCommand(ctx context.Context, deviceNumber, deviceName string, randomFactor, battery int, randomFactorData string) (*MintResult, error) {
	out, err := c.rpc(ctx, "unlockCommand", deviceNumber, deviceName, randomFactor, battery, randomFactorData)
	if err != nil {
		return nil, err
	}
	return decodeMint(out)
}

// BleUnlockMint mints an unlock frame directly from a raw BLE reply JSON.
func (c *Core) BleUnlockMint(ctx context.Context, deviceNumber, deviceName, replyJSON string) (*MintResult, error) {
	out, err := c.rpc(ctx, "bleUnlockMint", deviceNumber, deviceName, replyJSON)
	if err != nil {
		return nil, err
	}
	return decodeMint(out)
}

// BleSetPasswordMint mints a set-password-over-BLE frame from a raw BLE reply JSON.
func (c *Core) BleSetPasswordMint(ctx context.Context, deviceNumber, deviceName, replyJSON, password string, startTime, endTime, times int64, user, remark string) (*MintResult, error) {
	out, err := c.rpc(ctx, "bleSetPasswordMint", deviceNumber, deviceName, replyJSON, password, startTime, endTime, times, user, remark)
	if err != nil {
		return nil, err
	}
	return decodeMint(out)
}

// BleAddCardMint mints an add-card-over-BLE frame from a raw BLE reply JSON.
func (c *Core) BleAddCardMint(ctx context.Context, deviceNumber, deviceName, replyJSON, cardText string, startTime, endTime int64, cardType int, user string) (*MintResult, error) {
	out, err := c.rpc(ctx, "bleAddCardMint", deviceNumber, deviceName, replyJSON, cardText, startTime, endTime, cardType, user)
	if err != nil {
		return nil, err
	}
	return decodeMint(out)
}

// BleAddFingerprintMint mints an add-fingerprint-over-BLE frame from a raw BLE reply JSON.
func (c *Core) BleAddFingerprintMint(ctx context.Context, deviceNumber, deviceName, replyJSON, user string) (*MintResult, error) {
	out, err := c.rpc(ctx, "bleAddFingerprintMint", deviceNumber, deviceName, replyJSON, user)
	if err != nil {
		return nil, err
	}
	return decodeMint(out)
}

// SetPasswordCommand mints a set-password frame for a locally-read nonce.
func (c *Core) SetPasswordCommand(ctx context.Context, deviceNumber, deviceName string, randomFactor, battery int, password string, startTime, endTime, times int64, user, remark string) (*MintResult, error) {
	out, err := c.rpc(ctx, "setPasswordCommand", deviceNumber, deviceName, randomFactor, battery, password, startTime, endTime, times, user, remark)
	if err != nil {
		return nil, err
	}
	return decodeMint(out)
}

// ReportCommandResult acknowledges a minted command's serverID back to the cloud.
func (c *Core) ReportCommandResult(ctx context.Context, serverID string) error {
	return c.rpcErr(ctx, "reportCommandResult", serverID)
}

// decodeMint parses a mint JSON reply into a MintResult.
func decodeMint(out string) (*MintResult, error) {
	var m MintResult
	if err := decode(out, &m); err != nil {
		return nil, err
	}
	return &m, nil
}

// LockStatus returns the loose lock-status object.
func (c *Core) LockStatus(ctx context.Context, deviceNumber, deviceName string) (LockStatus, error) {
	out, err := c.rpc(ctx, "lockStatus", deviceNumber, deviceName)
	if err != nil {
		return nil, err
	}
	var ls LockStatus
	if err := decode(out, &ls); err != nil {
		return nil, err
	}
	return ls, nil
}

// CommandStatus polls the loose status object for a command id.
func (c *Core) CommandStatus(ctx context.Context, value string) (CommandStatus, error) {
	out, err := c.rpc(ctx, "commandStatus", value)
	if err != nil {
		return nil, err
	}
	var cs CommandStatus
	if err := decode(out, &cs); err != nil {
		return nil, err
	}
	return cs, nil
}

// RemoteLockInfo returns the loose remote battery/signal object.
func (c *Core) RemoteLockInfo(ctx context.Context, deviceNumber string) (RemoteLockInfo, error) {
	out, err := c.rpc(ctx, "remoteLockInfo", deviceNumber)
	if err != nil {
		return nil, err
	}
	var ri RemoteLockInfo
	if err := decode(out, &ri); err != nil {
		return nil, err
	}
	return ri, nil
}

// TriggerSwitch flips a gateway switch. Returns the raw reply string.
func (c *Core) TriggerSwitch(ctx context.Context, value string) (string, error) {
	out, err := c.rpc(ctx, "triggerSwitch", value)
	return strings.TrimSpace(out), err
}

// TriggerSwitchDelay flips a gateway switch after a delay. Returns the raw reply string.
func (c *Core) TriggerSwitchDelay(ctx context.Context, gateway string, delay int) (string, error) {
	out, err := c.rpc(ctx, "triggerSwitchDelay", gateway, delay)
	return strings.TrimSpace(out), err
}

// --- gateways -------------------------------------------------------------

// AddGatewayQr registers a gateway from a scanned QR payload. Returns the raw reply.
func (c *Core) AddGatewayQr(ctx context.Context, qrCode string) (string, error) {
	return c.rpc(ctx, "addGatewayQr", qrCode)
}

// AddGateway registers a gateway by name/mac/remark. Returns the raw reply.
func (c *Core) AddGateway(ctx context.Context, name, mac, remark string) (string, error) {
	return c.rpc(ctx, "addGateway", name, mac, remark)
}

// GatewaysStatus returns the status object for a gateway value. Returns the raw reply.
func (c *Core) GatewaysStatus(ctx context.Context, value string) (string, error) {
	return c.rpc(ctx, "gatewaysStatus", value)
}

// Gateways lists the account's gateways.
func (c *Core) Gateways(ctx context.Context) ([]Gateway, error) {
	out, err := c.rpc(ctx, "gateways")
	if err != nil {
		return nil, err
	}
	var gws []Gateway
	if err := decode(out, &gws); err != nil {
		return nil, err
	}
	return gws, nil
}

// AlterGatewayName renames a gateway.
func (c *Core) AlterGatewayName(ctx context.Context, name, gateway string) error {
	return c.rpcErr(ctx, "alterGatewayName", name, gateway)
}

// DeleteGateway unbinds a gateway.
func (c *Core) DeleteGateway(ctx context.Context, value string) error {
	return c.rpcErr(ctx, "deleteGateway", value)
}

// GatewayLockName resolves {"deviceName","gateway"} for a lock reachable via a gateway.
func (c *Core) GatewayLockName(ctx context.Context, deviceNumber string) (string, error) {
	return c.rpc(ctx, "gatewayLockName", deviceNumber)
}

// --- uFun gateway credentials (all return a BARE command id — poll CommandStatus) --

// UfunSetPassword sets a password via the uFun gateway. Returns the bare command id.
func (c *Core) UfunSetPassword(ctx context.Context, deviceNumber, deviceName, value string, times int, startTimestamp, endTimestamp int64, remark string) (string, error) {
	out, err := c.rpc(ctx, "ufunSetPassword", deviceNumber, deviceName, value, times, startTimestamp, endTimestamp, remark)
	return strings.TrimSpace(out), err
}

// UfunDeletePassword deletes a password via the uFun gateway. Returns the bare command id.
func (c *Core) UfunDeletePassword(ctx context.Context, deviceNumber, deviceName, value string) (string, error) {
	out, err := c.rpc(ctx, "ufunDeletePassword", deviceNumber, deviceName, value)
	return strings.TrimSpace(out), err
}

// UfunAddCard adds a card via the uFun gateway. Returns the bare command id.
func (c *Core) UfunAddCard(ctx context.Context, deviceNumber, deviceName, value string, startTimestamp, endTimestamp int64, condition int) (string, error) {
	out, err := c.rpc(ctx, "ufunAddCard", deviceNumber, deviceName, value, startTimestamp, endTimestamp, condition)
	return strings.TrimSpace(out), err
}

// UfunDeleteCard deletes a card (by window) via the uFun gateway. Returns the bare command id.
func (c *Core) UfunDeleteCard(ctx context.Context, deviceNumber, deviceName, value string, starttime, endtime int64, condition int) (string, error) {
	out, err := c.rpc(ctx, "ufunDeleteCard", deviceNumber, deviceName, value, starttime, endtime, condition)
	return strings.TrimSpace(out), err
}

// UfunDeleteCardNo deletes a card by number via the uFun gateway. Returns the bare command id.
func (c *Core) UfunDeleteCardNo(ctx context.Context, deviceNumber, deviceName, value string) (string, error) {
	out, err := c.rpc(ctx, "ufunDeleteCardNo", deviceNumber, deviceName, value)
	return strings.TrimSpace(out), err
}

// UfunDeleteFingerprint deletes a fingerprint via the uFun gateway. Returns the bare command id.
func (c *Core) UfunDeleteFingerprint(ctx context.Context, deviceNumber, deviceName, value string) (string, error) {
	out, err := c.rpc(ctx, "ufunDeleteFingerprint", deviceNumber, deviceName, value)
	return strings.TrimSpace(out), err
}

// UfunSetLockTime sets the lock's clock via the uFun gateway. Returns the bare command id.
func (c *Core) UfunSetLockTime(ctx context.Context, deviceNumber, deviceName, value string, timeOffset int) (string, error) {
	out, err := c.rpc(ctx, "ufunSetLockTime", deviceNumber, deviceName, value, timeOffset)
	return strings.TrimSpace(out), err
}

// --- members & permissions ------------------------------------------------

// Members lists the credentials (passwords/cards/fingerprints) on a lock.
func (c *Core) Members(ctx context.Context, deviceNumber, deviceName, user string) ([]Credential, error) {
	out, err := c.rpc(ctx, "members", deviceNumber, deviceName, user)
	if err != nil {
		return nil, err
	}
	var creds []Credential
	if err := decode(out, &creds); err != nil {
		return nil, err
	}
	return creds, nil
}

// MembersGrouped lists the lock's credentials grouped by owning user (grouping done in
// the helper — the client just decodes the []OwnerGroup and can never render ungrouped).
func (c *Core) MembersGrouped(ctx context.Context, deviceNumber, deviceName string) ([]OwnerGroup, error) {
	out, err := c.rpc(ctx, "membersByOwner", deviceNumber, deviceName)
	if err != nil {
		return nil, err
	}
	var groups []OwnerGroup
	if err := decode(out, &groups); err != nil {
		return nil, err
	}
	return groups, nil
}

// MemberOwners lists the distinct credential owners on a lock — the option set a user picker offers.
func (c *Core) MemberOwners(ctx context.Context, deviceNumber, deviceName string) ([]string, error) {
	out, err := c.rpc(ctx, "memberOwners", deviceNumber, deviceName)
	if err != nil {
		return nil, err
	}
	var owners []string
	if err := decode(out, &owners); err != nil {
		return nil, err
	}
	return owners, nil
}

// NextCredentialNumber suggests the next free credential number so an add form can prefill it.
func (c *Core) NextCredentialNumber(ctx context.Context, deviceNumber, deviceName string) (string, error) {
	out, err := c.rpc(ctx, "nextCredentialNumber", deviceNumber, deviceName)
	if err != nil {
		return "", err
	}
	return unquote(out), nil
}

// PermissionList lists the sharing permissions on a lock.
func (c *Core) PermissionList(ctx context.Context, deviceNumber string) ([]Permission, error) {
	out, err := c.rpc(ctx, "permissionList", deviceNumber)
	if err != nil {
		return nil, err
	}
	var perms []Permission
	if err := decode(out, &perms); err != nil {
		return nil, err
	}
	return perms, nil
}

// AddMember adds a credential (password/card/fingerprint) to a lock. Returns the raw reply.
func (c *Core) AddMember(ctx context.Context, deviceNumber, deviceName, credType, user, number string) (string, error) {
	return c.rpc(ctx, "addMember", deviceNumber, deviceName, credType, user, number)
}

// DeleteMember removes a credential from a lock.
func (c *Core) DeleteMember(ctx context.Context, deviceNumber, deviceName, credType, number string) error {
	return c.rpcErr(ctx, "deleteMember", deviceNumber, deviceName, credType, number)
}

// AddPermission shares a lock with another account. Returns the raw reply.
func (c *Core) AddPermission(ctx context.Context, deviceNumber, authorizedAcount string, roleID int, beginTime, endTime int64, unlockNumber int, remark string) (string, error) {
	return c.rpc(ctx, "addPermission", deviceNumber, authorizedAcount, roleID, beginTime, endTime, unlockNumber, remark)
}

// DelPermission revokes a shared permission.
func (c *Core) DelPermission(ctx context.Context, deviceNumber, authorizedAcount string) error {
	return c.rpcErr(ctx, "delPermission", deviceNumber, authorizedAcount)
}

// TransferPermission transfers ownership of a lock to another account.
func (c *Core) TransferPermission(ctx context.Context, deviceNumber, authorizedAcount, areacode string) error {
	return c.rpcErr(ctx, "transferPermission", deviceNumber, authorizedAcount, areacode)
}
