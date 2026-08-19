package app

import (
	"encoding/json"
	"testing"
)

// TestDeviceListItemModelTag guards the JSON tag that feeds ValidatePin: the raw devices
// list names the lock model "model" (not "deviceModel"). A mismatch here silently blanks
// DeviceModel, which skips the TOUCA keypad 0–6 rule and lets invalid PINs reach the lock.
func TestDeviceListItemModelTag(t *testing.T) {
	const raw = `{"type":1,"model":"TOUCAEBL51","deviceName":"Escale Door","deviceNumber":"25526842"}`
	var it DeviceListItem
	if err := json.Unmarshal([]byte(raw), &it); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if it.DeviceModel != "TOUCAEBL51" {
		t.Fatalf("DeviceModel = %q, want %q (JSON key is \"model\")", it.DeviceModel, "TOUCAEBL51")
	}
}
