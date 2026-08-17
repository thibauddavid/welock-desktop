// Package sidecar drives the engine helper process. The helper is an opaque,
// prebuilt binary (all protocol baked in) that the app embeds and spawns; the
// app talks to it over line-delimited JSON-RPC on the child's stdin/stdout. This is how
// welock-desktop stays a pure UI + BLE radio while importing NOTHING from the engine —
// the native analog of a web client loading a prebuilt wasm module.
package sidecar

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sync"
)

// RPCError is a failure returned by the helper. Code carries the welock result code; 1000
// is the single-session token-expired signal the app treats as "re-login".
type RPCError struct {
	Msg  string
	Code int
}

func (e *RPCError) Error() string { return e.Msg }

type request struct {
	ID     int    `json:"id"`
	Method string `json:"method"`
	Args   []any  `json:"args"`
}

type tokenPair struct {
	Access  string `json:"access"`
	Refresh string `json:"refresh"`
}

type response struct {
	ID     int        `json:"id"`
	Result string     `json:"result"`
	Error  string     `json:"error"`
	Code   int        `json:"code"`
	Tokens *tokenPair `json:"tokens"`
}

// Client is a running helper process and the JSON-RPC pipe to it. Requests are
// multiplexed: each Call registers a response channel keyed by request id, a single
// reader goroutine routes responses back, and writes are serialized by writeMu. This
// lets a fast pure/BLE-frame call return while a slow cloud call is still in flight
// (the sidecar handles those concurrently too).
type Client struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	writeMu sync.Mutex

	mu       sync.Mutex
	id       int
	pending  map[int]chan response
	deviceID string
	access   string
	refresh  string
	closed   bool

	closeOnce sync.Once
	closeErr  error
}

// Start locates the helper (the WELOCK_SIDECAR env override, else the embedded binary
// extracted to the user cache), spawns it, and runs the init handshake which pins the
// session to deviceID/platform/baseURL + tokens and returns the (possibly generated)
// device id. The helper's stderr is passed through for verbose logging.
func Start(baseURL, deviceID, accessToken, refreshToken string) (*Client, error) {
	path, err := locate()
	if err != nil {
		return nil, err
	}
	cmd := exec.Command(path)
	cmd.Stderr = os.Stderr

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("sidecar: start %s: %w", path, err)
	}

	c := &Client{cmd: cmd, stdin: stdin, pending: map[int]chan response{}}
	go c.readLoop(bufio.NewReaderSize(stdout, 1<<20))

	// init(deviceID, platform, baseURL, access, refresh) -> deviceID (+ echoed tokens).
	id, err := c.Call("init", deviceID, "ios", baseURL, accessToken, refreshToken)
	if err != nil {
		_ = c.Close()
		return nil, err
	}
	c.mu.Lock()
	c.deviceID = id
	c.mu.Unlock()
	return c, nil
}

// readLoop is the sole reader of the helper's stdout. It routes each response to the
// waiting Call by id and updates the cached tokens. On EOF/error it fails all pending
// calls so no caller blocks forever.
func (c *Client) readLoop(r *bufio.Reader) {
	for {
		raw, err := r.ReadBytes('\n')
		if len(raw) > 0 {
			var resp response
			if json.Unmarshal(raw, &resp) == nil {
				c.mu.Lock()
				if resp.Tokens != nil {
					c.access = resp.Tokens.Access
					c.refresh = resp.Tokens.Refresh
				}
				ch := c.pending[resp.ID]
				delete(c.pending, resp.ID)
				c.mu.Unlock()
				if ch != nil {
					ch <- resp
				}
			}
		}
		if err != nil {
			c.mu.Lock()
			c.closed = true
			for id, ch := range c.pending {
				close(ch)
				delete(c.pending, id)
			}
			c.mu.Unlock()
			return
		}
	}
}

// Call sends one request and returns the helper's result string (the exact JSON — or bare
// string — mobile.Session would have returned) along with any error. Concurrent calls are
// safe; each waits for its own id's response.
func (c *Client) Call(method string, args ...any) (string, error) {
	if args == nil {
		args = []any{}
	}
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return "", errors.New("sidecar: client closed")
	}
	c.id++
	id := c.id
	ch := make(chan response, 1)
	c.pending[id] = ch
	c.mu.Unlock()

	line, err := json.Marshal(request{ID: id, Method: method, Args: args})
	if err != nil {
		c.forget(id)
		return "", err
	}
	c.writeMu.Lock()
	_, werr := c.stdin.Write(append(line, '\n'))
	c.writeMu.Unlock()
	if werr != nil {
		c.forget(id)
		return "", fmt.Errorf("sidecar: write: %w", werr)
	}

	resp, ok := <-ch
	if !ok {
		return "", errors.New("sidecar: helper exited")
	}
	if resp.Error != "" {
		return "", &RPCError{Msg: resp.Error, Code: resp.Code}
	}
	return resp.Result, nil
}

// forget drops a pending request that never made it onto the wire.
func (c *Client) forget(id int) {
	c.mu.Lock()
	delete(c.pending, id)
	c.mu.Unlock()
}

// SetTokens imports a token pair into the helper's session and caches it.
func (c *Client) SetTokens(access, refresh string) error {
	_, err := c.Call("setTokens", access, refresh)
	return err
}

// DeviceID returns the immutable device id the session is pinned to (from init).
func (c *Client) DeviceID() string { return c.deviceID }

// AccessToken / RefreshToken return the last tokens the helper echoed (updated after
// every call, so any auto-refresh is reflected).
func (c *Client) AccessToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.access
}

func (c *Client) RefreshToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.refresh
}

// Version returns the engine release the helper was built from (main.coreVersion),
// so the host can assert helper==pin at spawn.
func (c *Client) Version() (string, error) { return c.Call("version") }

// Close shuts the helper down by closing its stdin (the read loop exits on EOF) and
// reaping the process. Idempotent.
func (c *Client) Close() error {
	c.mu.Lock()
	c.closed = true
	c.mu.Unlock()

	c.closeOnce.Do(func() {
		_ = c.stdin.Close()
		if c.cmd != nil && c.cmd.Process != nil {
			c.closeErr = c.cmd.Wait()
		}
	})
	return c.closeErr
}
