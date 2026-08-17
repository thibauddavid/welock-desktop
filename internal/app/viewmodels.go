package app

// PinEntry mirrors the engine's bff.KeypadPin: one keypad PIN in the unified list the core
// produces by merging WeLock's two backend PIN lists (permanent member PINs, deletable by
// Number; and time-limited gateway PINs, deletable by Index). The merge lives in the core;
// the client just decodes this.
type PinEntry struct {
	Owner     string `json:"owner"`
	Key       string `json:"key"`       // delete key: the gateway Index, or the member Number
	ByIndex   bool   `json:"byIndex"`   // true: gateway delete (UfunDeletePassword by Index); false: DeleteMember by Number
	EndTime   int64  `json:"endTime"`   // 0 = permanent
	Permanent bool   `json:"permanent"` // no expiry (computed in the core: 0 or a far-future sentinel)
}

// This file mirrors the engine's bff view-model structs (and ble.Status) as
// LOCAL types, so the app decodes the sidecar's JSON replies without importing any
// engine package. It is the Go analog of a web client's api/types.ts: the shaping
// still happens in the core (now the sidecar), the client only names the resulting shape.
// Field names and json tags are kept identical to the core structs.

// Device is the shaped lock DeviceInfo returns: identity fields plus a computed battery
// and decoded capabilities.
type Device struct {
	DeviceNumber string        `json:"deviceNumber"`
	DeviceName   string        `json:"deviceName"`
	DeviceModel  string        `json:"deviceModel,omitempty"`
	Battery      *int          `json:"battery,omitempty"`      // MAX(ble[].power); pointer so a real 0% ≠ unknown
	Capabilities *Capabilities `json:"capabilities,omitempty"` // omitted on list entries (no features[])
}

// Capabilities are the typed feature flags decoded from the packed features[] strings.
type Capabilities struct {
	SupportsGatewayUnlock bool `json:"supportsGatewayUnlock"`
	CanGatewayAddPin      bool `json:"canGatewayAddPin"`
	CanGatewayAddCard     bool `json:"canGatewayAddCard"`
	CanBleAddPin          bool `json:"canBleAddPin"`
	CanBleAddCard         bool `json:"canBleAddCard"`
	CanBleAddFingerprint  bool `json:"canBleAddFingerprint"`
}

// Credential is one enrolled credential, flattened from the user-grouped Members response.
type Credential struct {
	Owner             string `json:"owner"`
	TypeName          string `json:"typeName"`
	TypeLabel         string `json:"typeLabel"`
	Number            string `json:"number"` // the value displayed
	Index             string `json:"index"`  // the SLOT — what UfunDeletePassword deletes by (≠ number)
	ID                string `json:"id"`     // server record id
	CreationTime      string `json:"creationTime"`
	EndTime           int64  `json:"endTime"` // 0 = no expiry
	GatewayManageable bool   `json:"gatewayManageable"`
}

// CredentialType is one selectable credential kind. Name is the exact API type name the
// endpoints validate against, so the UI renders pickers from this list, never hardcoded.
type CredentialType struct {
	Name              string `json:"name"`
	Label             string `json:"label"`
	GatewayManageable bool   `json:"gatewayManageable"`
	BleAddable        bool   `json:"bleAddable"`
}

// OwnerGroup is a person and their credentials — the per-person view the Manage tab renders.
type OwnerGroup struct {
	User  string       `json:"user"`
	Creds []Credential `json:"creds"`
}

// Permission is one cloud authorization (a shared-access grant) on a lock.
type Permission struct {
	Account      string `json:"account"`
	NickName     string `json:"nickName,omitempty"`
	RoleID       int    `json:"roleID"`
	BeginTime    int64  `json:"beginTime"`
	EndTime      int64  `json:"endTime"`      // 0 = no expiry
	UnlockNumber int    `json:"unlockNumber"` // 0 = unlimited
	Remark       string `json:"remark,omitempty"`
}

// UnlockRecord is one shaped row of the lock's unlock history.
type UnlockRecord struct {
	ID             string `json:"id"`
	ActorID        string `json:"actorId"`
	Name           string `json:"name"`
	How            string `json:"how"`
	Time           string `json:"time"`
	Remote         bool   `json:"remote"`
	UnlockResult   int    `json:"unlockResult"`
	OperatingState int    `json:"operatingState"`
}

// Gateway is a shaped gateway for the Gateways screen (owned type-6 box, or a read-only
// bridging gateway discovered via a lock's status).
type Gateway struct {
	DeviceNumber string `json:"deviceNumber,omitempty"`
	DeviceName   string `json:"deviceName"`
	GatewayModel string `json:"gatewayModel,omitempty"`
	GwType       string `json:"gwType,omitempty"`
	Owned        bool   `json:"owned"`
	Online       bool   `json:"online"`
	Bridges      string `json:"bridges,omitempty"`
}

// ValidityPreset is one credential/offline-code validity option offered to the user.
type ValidityPreset struct {
	Key     string `json:"key"`
	Label   string `json:"label"`
	Seconds int64  `json:"seconds"` // 0 for "permanent"
}

// BleStatus is the parsed reply to a status query (the app reads the frame over BLE and
// the sidecar parses it). It mirrors welock/ble.Status's rendered fields.
type BleStatus struct {
	RandomFactor int `json:"randomFactor"`
	Battery      int `json:"battery"`
}
