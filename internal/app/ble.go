//go:build tinygobt

package app

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
)

// This file is the direct-BLE glue for the radio build. The radio (ble_radio.go) writes
// raw frames and streams raw notifications; every protocol step — which query bytes to
// send, which notification is the reply, how to parse it, and the cloud mint — is a
// sidecar RPC. No protocol logic lives here (the same contract a web client honors).

// replyWindow is how long a read waits for the lock's notification reply.
const replyWindow = 6 * time.Second

// BleAvailable reports whether this build has a real BLE radio (it does).
func (c *Core) BleAvailable() bool { return true }

// BleScan discovers nearby WeLock peripherals.
func (c *Core) BleScan(ctx context.Context) ([]BleDevice, error) { return bleScan(ctx) }

// --- frame helpers (all protocol lives in the sidecar) --------------------

func frameInts(b []byte) []int {
	out := make([]int, len(b))
	for i, v := range b {
		out[i] = int(v)
	}
	return out
}

func bytesOf(ints []int) []byte {
	b := make([]byte, len(ints))
	for i, v := range ints {
		b[i] = byte(v)
	}
	return b
}

// replyFrameJSON renders a raw notification frame as the JSON int array the cloud mint
// endpoints expect as replyJSON.
func replyFrameJSON(frame []byte) string {
	b, _ := json.Marshal(frameInts(frame))
	return string(b)
}

// frameQuery fetches a constant query frame (batteryQuery / statusQuery) from the sidecar.
func (c *Core) frameQuery(method string) ([]byte, error) {
	out, err := c.client.Call(method)
	if err != nil {
		return nil, err
	}
	var ints []int
	if err := json.Unmarshal([]byte(out), &ints); err != nil {
		return nil, err
	}
	return bytesOf(ints), nil
}

// isNonceReply asks the sidecar whether a frame is the 55 30 nonce reply to await.
func (c *Core) isNonceReply(frame []byte) bool {
	out, err := c.client.Call("isNonceReply", frameInts(frame))
	return err == nil && strings.TrimSpace(out) == "true"
}

type bleNonce struct {
	RandomFactor     int    `json:"randomFactor"`
	Battery          int    `json:"battery"`
	RandomFactorData string `json:"randomFactorData"`
}

// parseNonce parses a 55 30 reply frame (via the sidecar) into its nonce fields.
func (c *Core) parseNonce(frame []byte) (*bleNonce, error) {
	out, err := c.client.Call("parseNonce", frameInts(frame))
	if err != nil {
		return nil, err
	}
	var n bleNonce
	if err := json.Unmarshal([]byte(out), &n); err != nil {
		return nil, err
	}
	return &n, nil
}

// parseStatus asks the sidecar to parse a device-status (55 44) reply; ok=false means the
// frame is not a status reply (so it doubles as the await matcher).
func (c *Core) parseStatus(frame []byte) (BleStatus, bool) {
	out, err := c.client.Call("parseStatus", frameInts(frame))
	if err != nil {
		return BleStatus{}, false
	}
	var st BleStatus
	if err := json.Unmarshal([]byte(out), &st); err != nil {
		return BleStatus{}, false
	}
	return st, true
}

// awaitReply reads notification frames until match returns true, or ctx/timeout fires.
func awaitReply(ctx context.Context, conn *bleConn, match func([]byte) bool) ([]byte, error) {
	t := time.NewTimer(replyWindow)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-t.C:
			return nil, errors.New("ble: timed out waiting for the lock's reply")
		case f, ok := <-conn.Notifications():
			if !ok {
				return nil, errors.New("ble: connection closed before a reply")
			}
			if match(f) {
				return f, nil
			}
		}
	}
}

// readNonceFrame sends the 55 30 query and returns the raw nonce reply frame.
func (c *Core) readNonceFrame(ctx context.Context, conn *bleConn) ([]byte, error) {
	q, err := c.frameQuery("batteryQuery")
	if err != nil {
		return nil, err
	}
	if err := conn.Write(ctx, q); err != nil {
		return nil, err
	}
	return awaitReply(ctx, conn, c.isNonceReply)
}

// --- read actions (no cloud) ----------------------------------------------

// BleReadStatus connects and reads the device-status (55 44) frame: random factor + battery.
func (c *Core) BleReadStatus(ctx context.Context, address string) (*BleStatus, error) {
	conn, err := bleConnect(ctx, address)
	if err != nil {
		return nil, err
	}
	defer conn.Close()

	q, err := c.frameQuery("statusQuery")
	if err != nil {
		return nil, err
	}
	if err := conn.Write(ctx, q); err != nil {
		return nil, err
	}
	var st BleStatus
	if _, err := awaitReply(ctx, conn, func(f []byte) bool {
		s, ok := c.parseStatus(f)
		if ok {
			st = s
		}
		return ok
	}); err != nil {
		return nil, err
	}
	return &st, nil
}

// BleReadBattery connects and reads the battery level from the reliable 55 30 nonce reply
// (battery = frame[2] on the nRF units, the value the unlock path also uses).
func (c *Core) BleReadBattery(ctx context.Context, address string) (int, error) {
	conn, err := bleConnect(ctx, address)
	if err != nil {
		return 0, err
	}
	defer conn.Close()
	frame, err := c.readNonceFrame(ctx, conn)
	if err != nil {
		return 0, err
	}
	n, err := c.parseNonce(frame)
	if err != nil {
		return 0, err
	}
	return n.Battery, nil
}

// --- unlock + credential mints (read reply → mint → write → report) -------

// BleUnlock performs a direct over-the-air unlock: read the per-connection nonce, have the
// sidecar mint the unlock frame bound to it, write the frame, then report the result.
func (c *Core) BleUnlock(ctx context.Context, address, deviceNumber, deviceName string) error {
	return c.bleProvision(ctx, address, func(replyJSON string) (*MintResult, error) {
		return c.BleUnlockMint(ctx, deviceNumber, deviceName, replyJSON)
	})
}

// bleProvision is the shared read-reply → mint → write → report glue for unlock and every
// credential mint. mint receives the raw 55 30 reply frame as replyJSON; the sidecar parses
// the nonce and mints the command. No protocol logic lives here.
func (c *Core) bleProvision(ctx context.Context, address string, mint func(replyJSON string) (*MintResult, error)) error {
	conn, err := bleConnect(ctx, address)
	if err != nil {
		return err
	}
	defer conn.Close()

	frame, err := c.readNonceFrame(ctx, conn)
	if err != nil {
		return err
	}
	m, err := mint(replyFrameJSON(frame))
	if err != nil {
		return err
	}
	if err := conn.Write(ctx, m.Command()); err != nil {
		return err
	}
	if m.ServerID != "" {
		return c.ReportCommandResult(ctx, m.ServerID)
	}
	return nil
}

// BleSetPassword adds a PIN over direct BLE.
func (c *Core) BleSetPassword(ctx context.Context, address, deviceNumber, deviceName, password string, startTime, endTime, times int64, user, remark string) error {
	return c.bleProvision(ctx, address, func(replyJSON string) (*MintResult, error) {
		return c.BleSetPasswordMint(ctx, deviceNumber, deviceName, replyJSON, password, startTime, endTime, times, user, remark)
	})
}

// BleAddCard adds a card over direct BLE.
func (c *Core) BleAddCard(ctx context.Context, address, deviceNumber, deviceName, cardText string, startTime, endTime int64, cardType int, user string) error {
	return c.bleProvision(ctx, address, func(replyJSON string) (*MintResult, error) {
		return c.BleAddCardMint(ctx, deviceNumber, deviceName, replyJSON, cardText, startTime, endTime, cardType, user)
	})
}

// BleAddFingerprint starts a fingerprint enrolment over direct BLE (captured at the sensor
// after the command is written).
func (c *Core) BleAddFingerprint(ctx context.Context, address, deviceNumber, deviceName, user string) error {
	return c.bleProvision(ctx, address, func(replyJSON string) (*MintResult, error) {
		return c.BleAddFingerprintMint(ctx, deviceNumber, deviceName, replyJSON, user)
	})
}
