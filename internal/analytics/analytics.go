// Package analytics is a minimal, fire-and-forget Amplitude HTTP V2 client for anonymous
// product telemetry (how many people use the app, retention, which features get used).
//
// It sends ONLY non-identifying data: a random per-install id, the app version, the OS,
// and low-cardinality event names/properties. It never sends the WeLock account, phone,
// tokens, device ids, lock names, or any credential value (PINs, cards, fingerprints).
//
// The API key is injected at release-build time via
//
//	-ldflags "-X github.com/thibauddavid/welock-desktop/internal/analytics.apiKey=<KEY>"
//
// so source checkouts, dev builds, and forks carry no key and send nothing at all —
// analytics is a hard no-op unless a key is baked in AND the user has left it enabled.
package analytics

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

// apiKey is set at release-build time via -ldflags -X (see the package doc). Empty in
// source/dev/fork builds → analytics is a permanent no-op.
var apiKey string

// Amplitude ingestion endpoints, by data region. Amplitude orgs are US-region unless the
// org was explicitly created as EU (a US key is rejected by the EU endpoint and vice
// versa), so US is the default. For an EU-region org, override at build time with:
//
//	-ldflags "-X github.com/thibauddavid/welock-desktop/internal/analytics.endpointOverride=https://api.eu.amplitude.com/2/httpapi"
const (
	usEndpoint = "https://api2.amplitude.com/2/httpapi"
	euEndpoint = "https://api.eu.amplitude.com/2/httpapi"
)

// endpointOverride, when injected at build time, replaces the default (US) endpoint —
// used to point a build at an EU-region org without a code change.
var endpointOverride string

// defaultEndpoint resolves the ingestion URL: the build-time override if set, else US.
func defaultEndpoint() string {
	if endpointOverride != "" {
		return endpointOverride
	}
	return usEndpoint
}

// Client sends events to Amplitude, fire-and-forget. A nil Client, an empty key, or a
// disabled client all send nothing.
type Client struct {
	key       string
	endpoint  string
	installID string
	platform  string
	sessionID int64

	enabled atomic.Bool
	version atomic.Value // string
	httpc   *http.Client
	debug   bool
}

// New builds a client for the given anonymous install id. enabled is the persisted user
// preference; it is forced off when no key is baked into the build.
func New(installID string, enabled bool) *Client {
	c := &Client{
		key:       apiKey,
		endpoint:  defaultEndpoint(),
		installID: installID,
		platform:  osName(),
		sessionID: time.Now().UnixMilli(),
		httpc:     &http.Client{Timeout: 5 * time.Second},
		debug:     os.Getenv("WELOCK_ANALYTICS_DEBUG") == "1",
	}
	c.version.Store("")
	c.enabled.Store(enabled && apiKey != "" && installID != "")
	return c
}

// Available reports whether a key is baked into this build — i.e. whether analytics is
// even possible here. The UI uses it to decide whether to show the opt-out toggle.
func (c *Client) Available() bool { return c != nil && c.key != "" }

// Enabled reports whether events are currently being sent.
func (c *Client) Enabled() bool { return c != nil && c.enabled.Load() }

// SetEnabled turns sending on/off (forced off when no key is baked in).
func (c *Client) SetEnabled(on bool) {
	if c == nil {
		return
	}
	c.enabled.Store(on && c.key != "" && c.installID != "")
}

// SetVersion records the app version reported with every event.
func (c *Client) SetVersion(v string) {
	if c != nil {
		c.version.Store(v)
	}
}

// Track sends one event, fire-and-forget. props must contain ONLY non-identifying,
// low-cardinality values (e.g. {"transport":"ble"}) — never account/lock/credential data.
// Failures are swallowed: telemetry must never affect the app.
func (c *Client) Track(event string, props map[string]any) {
	if c == nil {
		return
	}
	if c.key == "" {
		c.debugf("skip %q: no analytics key baked into this build", event)
		return
	}
	if !c.enabled.Load() {
		c.debugf("skip %q: analytics disabled (opt-out or empty install id)", event)
		return
	}
	version, _ := c.version.Load().(string)
	ev := amplitudeEvent{
		UserID:          c.installID,
		DeviceID:        c.installID,
		EventType:       event,
		Time:            time.Now().UnixMilli(),
		SessionID:       c.sessionID,
		AppVersion:      version,
		Platform:        c.platform,
		OSName:          c.platform,
		EventProperties: props,
	}
	body, err := json.Marshal(amplitudePayload{APIKey: c.key, Events: []amplitudeEvent{ev}})
	if err != nil {
		c.debugf("skip %q: marshal error: %v", event, err)
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint, bytes.NewReader(body))
		if err != nil {
			c.debugf("send %q: request build error: %v", event, err)
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := c.httpc.Do(req)
		if err != nil {
			c.debugf("send %q: POST error: %v", event, err)
			return
		}
		defer resp.Body.Close()
		if c.debug {
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
			c.debugf("send %q -> HTTP %d: %s", event, resp.StatusCode, strings.TrimSpace(string(b)))
		}
	}()
}

// debugf writes a diagnostic line to stderr when WELOCK_ANALYTICS_DEBUG=1. It NEVER logs
// the API key or any event property value — only the event name, the outcome, and the
// Amplitude HTTP status/body (which carries no secrets).
func (c *Client) debugf(format string, args ...any) {
	if c == nil || !c.debug {
		return
	}
	fmt.Fprintf(os.Stderr, "welock-analytics: "+format+"\n", args...)
}

// osName maps GOOS to a friendly platform label Amplitude groups by.
func osName() string {
	switch runtime.GOOS {
	case "darwin":
		return "macos"
	case "windows":
		return "windows"
	default:
		return runtime.GOOS
	}
}

type amplitudePayload struct {
	APIKey string           `json:"api_key"`
	Events []amplitudeEvent `json:"events"`
}

type amplitudeEvent struct {
	UserID          string         `json:"user_id"`
	DeviceID        string         `json:"device_id"`
	EventType       string         `json:"event_type"`
	Time            int64          `json:"time"`
	SessionID       int64          `json:"session_id,omitempty"`
	AppVersion      string         `json:"app_version,omitempty"`
	Platform        string         `json:"platform,omitempty"`
	OSName          string         `json:"os_name,omitempty"`
	EventProperties map[string]any `json:"event_properties,omitempty"`
}
