//go:build !tinygobt

package app

import (
	"context"
	"errors"
)

// errNoRadio is returned by every BLE method in the stub (radio-less) build.
var errNoRadio = errors.New("BLE needs a radio: rebuild with -tags tinygobt")

// BleAvailable reports whether this build has a real BLE radio (it does not).
func (c *Core) BleAvailable() bool { return false }

// BleScan is unavailable without the radio build tag.
func (c *Core) BleScan(ctx context.Context) ([]BleDevice, error) { return nil, errNoRadio }

// BleReadStatus is unavailable without the radio build tag.
func (c *Core) BleReadStatus(ctx context.Context, address string) (*BleStatus, error) {
	return nil, errNoRadio
}

// BleReadBattery is unavailable without the radio build tag.
func (c *Core) BleReadBattery(ctx context.Context, address string) (int, error) {
	return 0, errNoRadio
}

// BleUnlock is unavailable without the radio build tag.
func (c *Core) BleUnlock(ctx context.Context, address, deviceNumber, deviceName string) error {
	return errNoRadio
}

// BleSetPassword is unavailable without the radio build tag.
func (c *Core) BleSetPassword(ctx context.Context, address, deviceNumber, deviceName, password string, startTime, endTime, times int64, user, remark string) error {
	return errNoRadio
}

// BleAddCard is unavailable without the radio build tag.
func (c *Core) BleAddCard(ctx context.Context, address, deviceNumber, deviceName, cardText string, startTime, endTime int64, cardType int, user string) error {
	return errNoRadio
}

// BleAddFingerprint is unavailable without the radio build tag.
func (c *Core) BleAddFingerprint(ctx context.Context, address, deviceNumber, deviceName, user string) error {
	return errNoRadio
}
