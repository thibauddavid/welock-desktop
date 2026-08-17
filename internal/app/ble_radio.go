//go:build tinygobt

package app

// This file is the app's native BLE radio: scan for WeLock peers and open a GATT link
// (FEE0/FEE1 write+notify) over tinygo.org/x/bluetooth — CoreBluetooth on macOS, WinRT on
// Windows. It contains ZERO protocol: it only writes raw frames and streams raw
// notification frames. All frame encode/decode + the cloud mint live in the sidecar. It
// is the desktop analog of a web client's webBluetooth.ts.

import (
	"context"
	"fmt"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

// GATT identifiers — the lock's primary write+notify pair, and how WeLock peers advertise.
// These are public, advertised UUIDs (not protocol secrets).
const (
	bleServiceUUID = "0000fee0-0000-1000-8000-00805f9b34fb"
	bleCharUUID    = "0000fee1-0000-1000-8000-00805f9b34fb"
	bleNamePrefix  = "WL"
)

// defaultScanWindow is how long Scan/Connect listen when the context carries no deadline.
const defaultScanWindow = 5 * time.Second

var (
	bleAdapter = bluetooth.DefaultAdapter
	enableOnce sync.Once
	enableErr  error
)

func enableAdapter() error {
	enableOnce.Do(func() { enableErr = bleAdapter.Enable() })
	return enableErr
}

// bleScan discovers WeLock peers advertising a name starting with bleNamePrefix, keeping
// the strongest RSSI seen per address.
func bleScan(ctx context.Context) ([]BleDevice, error) {
	if err := enableAdapter(); err != nil {
		return nil, fmt.Errorf("ble: enable adapter: %w", err)
	}
	var (
		mu    sync.Mutex
		found = map[string]BleDevice{}
	)
	collect := func(_ *bluetooth.Adapter, sr bluetooth.ScanResult) {
		name := sr.LocalName()
		if len(name) < len(bleNamePrefix) || name[:len(bleNamePrefix)] != bleNamePrefix {
			return
		}
		addr := sr.Address.String()
		mu.Lock()
		if prev, ok := found[addr]; !ok || int(sr.RSSI) > prev.RSSI {
			found[addr] = BleDevice{Name: name, Address: addr, RSSI: int(sr.RSSI)}
		}
		mu.Unlock()
	}
	if err := scanFor(ctx, collect); err != nil {
		return nil, err
	}
	mu.Lock()
	defer mu.Unlock()
	out := make([]BleDevice, 0, len(found))
	for _, d := range found {
		out = append(out, d)
	}
	return out, nil
}

// scanFor runs adapter.Scan in a goroutine and stops it when ctx is done or the scan
// window elapses.
func scanFor(ctx context.Context, cb func(*bluetooth.Adapter, bluetooth.ScanResult)) error {
	scanCtx := ctx
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		scanCtx, cancel = context.WithTimeout(ctx, defaultScanWindow)
		defer cancel()
	}
	errCh := make(chan error, 1)
	go func() { errCh <- bleAdapter.Scan(cb) }()
	select {
	case <-scanCtx.Done():
		_ = bleAdapter.StopScan()
		<-errCh
		if err := ctx.Err(); err != nil {
			return err
		}
		return nil
	case err := <-errCh:
		return err
	}
}

// bleConn is a connected GATT link to one lock over FEE0/FEE1.
type bleConn struct {
	dev  bluetooth.Device
	char bluetooth.DeviceCharacteristic

	notifs    chan []byte
	raw       chan []byte
	closed    chan struct{}
	closeOnce sync.Once
	closeErr  error
}

// bleConnect scans for the peer with the given address (a name starting with the prefix is
// also accepted), connects, discovers FEE0/FEE1 and subscribes to notifications.
func bleConnect(ctx context.Context, address string) (*bleConn, error) {
	if err := enableAdapter(); err != nil {
		return nil, fmt.Errorf("ble: enable adapter: %w", err)
	}

	var (
		mu      sync.Mutex
		target  *bluetooth.ScanResult
		matchCh = make(chan struct{}, 1)
	)
	find := func(_ *bluetooth.Adapter, sr bluetooth.ScanResult) {
		if sr.Address.String() != address && sr.LocalName() != address {
			return
		}
		mu.Lock()
		if target == nil {
			cp := sr
			target = &cp
			select {
			case matchCh <- struct{}{}:
			default:
			}
		}
		mu.Unlock()
	}

	scanCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if _, ok := ctx.Deadline(); !ok {
		var c2 context.CancelFunc
		scanCtx, c2 = context.WithTimeout(scanCtx, defaultScanWindow)
		defer c2()
	}

	errCh := make(chan error, 1)
	go func() { errCh <- bleAdapter.Scan(find) }()

	select {
	case <-matchCh:
		_ = bleAdapter.StopScan()
		<-errCh
	case <-scanCtx.Done():
		_ = bleAdapter.StopScan()
		<-errCh
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return nil, fmt.Errorf("ble: device %q not found within scan window", address)
	case err := <-errCh:
		if err != nil {
			return nil, fmt.Errorf("ble: scan: %w", err)
		}
		return nil, fmt.Errorf("ble: device %q not found", address)
	}

	mu.Lock()
	sr := *target
	mu.Unlock()

	dev, err := bleAdapter.Connect(sr.Address, bluetooth.ConnectionParams{})
	if err != nil {
		return nil, fmt.Errorf("ble: connect %q: %w", address, err)
	}
	c, err := newConn(dev)
	if err != nil {
		_ = dev.Disconnect()
		return nil, err
	}
	return c, nil
}

// newConn discovers FEE0/FEE1 on a connected device, subscribes to notifications, and
// starts the forwarder that owns the public channel.
func newConn(dev bluetooth.Device) (*bleConn, error) {
	svcUUID, err := bluetooth.ParseUUID(bleServiceUUID)
	if err != nil {
		return nil, fmt.Errorf("ble: parse service uuid: %w", err)
	}
	charUUID, err := bluetooth.ParseUUID(bleCharUUID)
	if err != nil {
		return nil, fmt.Errorf("ble: parse char uuid: %w", err)
	}
	svcs, err := dev.DiscoverServices([]bluetooth.UUID{svcUUID})
	if err != nil {
		return nil, fmt.Errorf("ble: discover service FEE0: %w", err)
	}
	if len(svcs) == 0 {
		return nil, fmt.Errorf("ble: service FEE0 not found on device")
	}
	chars, err := svcs[0].DiscoverCharacteristics([]bluetooth.UUID{charUUID})
	if err != nil {
		return nil, fmt.Errorf("ble: discover char FEE1: %w", err)
	}
	if len(chars) == 0 {
		return nil, fmt.Errorf("ble: characteristic FEE1 not found on device")
	}

	c := &bleConn{
		dev:    dev,
		char:   chars[0],
		notifs: make(chan []byte),
		raw:    make(chan []byte, 16),
		closed: make(chan struct{}),
	}
	err = c.char.EnableNotifications(func(buf []byte) {
		cp := make([]byte, len(buf)) // buf may be reused after this returns
		copy(cp, buf)
		select {
		case c.raw <- cp:
		case <-c.closed:
		}
	})
	if err != nil {
		return nil, fmt.Errorf("ble: subscribe FEE1 notifications: %w", err)
	}

	go func() {
		defer close(c.notifs)
		for {
			select {
			case <-c.closed:
				return
			case b := <-c.raw:
				select {
				case c.notifs <- b:
				case <-c.closed:
					return
				}
			}
		}
	}()
	return c, nil
}

// Write sends a command frame to FEE1, preferring WriteWithoutResponse (WeLock's own
// command path) and falling back to a with-response write.
func (c *bleConn) Write(ctx context.Context, frame []byte) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.closed:
		return fmt.Errorf("ble: write on closed transport")
	default:
	}
	if _, err := c.char.WriteWithoutResponse(frame); err != nil {
		if _, werr := c.char.Write(frame); werr != nil {
			return fmt.Errorf("ble: write FEE1: %w", werr)
		}
	}
	return nil
}

// Notifications returns the inbound frame stream; it is closed when Close runs.
func (c *bleConn) Notifications() <-chan []byte { return c.notifs }

// Close disconnects the link.
func (c *bleConn) Close() error {
	c.closeOnce.Do(func() {
		close(c.closed)
		c.closeErr = c.dev.Disconnect()
	})
	return c.closeErr
}
