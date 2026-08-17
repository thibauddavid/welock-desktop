// Package app is the backend layer of welock-desktop. It wraps a
// welock/mobile.Session together with an on-disk session store and exposes one
// typed method per feature. All protocol/business rules live in the engine; this
// package only marshals inputs, calls the Session (or the BLE radio), unmarshals
// the JSON reply into the appropriate welock/bff or local struct, and keeps the
// persisted tokens in sync. The UI holds ZERO protocol logic.
package app

// This file declares the local structs the shaped JSON unmarshals into that
// welock/bff does NOT provide. Shared model structs (bff.Device, bff.Credential,
// bff.Gateway, bff.Permission, bff.UnlockRecord, …) are imported from welock/bff
// and never re-declared here.

// TempPassword is one row of the shaped TempPasswords() array. welock/bff does not
// declare a TempPassword type, so it lives here.
type TempPassword struct {
	ID        string `json:"id"`
	Type      int    `json:"type"`
	StartTime int64  `json:"startTime"`
	EndTime   int64  `json:"endTime"`
	Time      string `json:"time"`
	Remark    string `json:"remark"`
	Password  string `json:"password"`
}

// DeviceListItem is one permissive row of the RAW Devices() array. Rows carry
// top-level fields and mix locks with gateways (type 6); Battery is a pointer
// because it may be absent. This intentionally mirrors the raw envelope rather
// than the shaped bff.Device.
type DeviceListItem struct {
	DeviceNumber string `json:"deviceNumber"`
	DeviceName   string `json:"deviceName"`
	Battery      *int   `json:"battery,omitempty"`
	Type         int    `json:"type,omitempty"`
	GatewayModel string `json:"gatewayModel,omitempty"`
	DeviceModel  string `json:"deviceModel,omitempty"`
}

// MintResult is the command "mint" the cloud returns for a BLE/remote credential
// command: a re-marshalled byte array plus the server id to report back. The field
// names are the mint's own ("bytes","serverID"), NOT welock.BleCommand's.
type MintResult struct {
	Bytes    []int  `json:"bytes"`
	ServerID string `json:"serverID"`
}

// Command converts the mint's []int payload into the []byte frame to write over BLE.
func (m *MintResult) Command() []byte {
	b := make([]byte, len(m.Bytes))
	for i, v := range m.Bytes {
		b[i] = byte(v)
	}
	return b
}

// WhatsAppLogin is the WhatsApp login challenge. StartWhatsAppLogin() returns JSON
// with CAPITALIZED keys (Code/Number/Message/URL); the json tags map them.
type WhatsAppLogin struct {
	Code    string `json:"Code"`
	Number  string `json:"Number"`
	Message string `json:"Message"`
	URL     string `json:"URL"`
}

// Tokens is the login/refresh token pair, as marshalled by the cloud surface
// (snake_case keys).
type Tokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token"`
}

// Loose shapes: these responses have no stable schema worth a struct, so they are
// decoded as generic maps and read key-by-key by the UI.

// LockStatus is the loose LockStatus() object.
type LockStatus map[string]any

// CommandStatus is the loose CommandStatus() object polled to a terminal state.
type CommandStatus map[string]any

// RemoteLockInfo is the loose RemoteLockInfo() object (remote battery/signal).
type RemoteLockInfo map[string]any

// BleDevice is a scanned BLE peripheral. It is declared in the default build so
// both build configs (stub and tinygobt) share the exact same type.
type BleDevice struct {
	Name    string
	Address string
	RSSI    int
}
