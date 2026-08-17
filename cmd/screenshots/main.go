//go:build screenshots

// Command screenshots renders one WeLock Desktop screen for a focus-free capture, backed by
// an in-process mock of the WeLock API returning anonymized fake data. It is gated behind the
// `screenshots` build tag and drives the ui package's preview helpers (internal/ui/preview.go).
//
// Build + run it via tools/screenshots/capture.sh, which loops every screen and assembles the
// README gallery + unlock GIF. Directly:
//
//	go build -tags "screenshots tinygobt" -o /tmp/welock-screens ./cmd/screenshots
//	HOME=/tmp/throwaway /tmp/welock-screens remote      # a feature screen
//	HOME=/tmp/throwaway /tmp/welock-screens modal:people # a modal, composed over the window
//	HOME=/tmp/throwaway /tmp/welock-screens unlockgif    # auto-taps Unlock for the GIF
//
// Run with HOME set to a throwaway dir so your real session/config is never touched.
package main

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	fyneapp "fyne.io/fyne/v2/app"

	"github.com/thibauddavid/welock-desktop/internal/app"
	"github.com/thibauddavid/welock-desktop/internal/ui"
)

func main() {
	ln, err := net.Listen("tcp", "127.0.0.1:8799")
	if err != nil {
		panic(err)
	}
	go http.Serve(ln, http.HandlerFunc(mock))

	fyneapp.SetMetadata(fyne.AppMetadata{ID: "com.welock.desktop", Name: "WeLock", Version: "0.1.0", Migrations: map[string]bool{"fyneDo": true}})
	a := fyneapp.New()
	a.Settings().SetTheme(ui.NewTheme())
	w := a.NewWindow("WeLock")
	w.Resize(fyne.NewSize(ui.DefaultWidth, ui.DefaultHeight))
	w.CenterOnScreen()

	core, err := app.New("http://127.0.0.1:8799/api")
	if err != nil {
		panic(err)
	}

	which := "locks"
	if len(os.Args) > 1 {
		which = os.Args[1]
	}

	switch {
	case which == "login":
		w.SetContent(ui.PreviewLogin(a, w, core))
	case which == "unlockgif":
		_ = core.LoginWithToken(context.Background(), "mock-access", "mock-refresh")
		w.SetContent(ui.PreviewMain(a, w, core, "remote"))
		go func() {
			time.Sleep(2500 * time.Millisecond)
			fyne.Do(func() { ui.PreviewTapUnlock(w) })
		}()
	case strings.HasPrefix(which, "modal:"):
		_ = core.LoginWithToken(context.Background(), "mock-access", "mock-refresh")
		w.SetContent(ui.PreviewMain(a, w, core, "manage"))
		modal := strings.TrimPrefix(which, "modal:")
		go func() {
			time.Sleep(1800 * time.Millisecond)
			fyne.Do(func() { ui.PreviewOpenModal(a, w, core, modal) })
		}()
	default:
		_ = core.LoginWithToken(context.Background(), "mock-access", "mock-refresh")
		w.SetContent(ui.PreviewMain(a, w, core, which))
	}
	w.ShowAndRun()
}

// cmdPolls makes CommandStatus "relay" for a couple of polls before confirming, so the
// unlock GIF shows Ready → relaying → succeeded rather than resolving instantly.
var cmdPolls int64

func mock(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Path == "/api/ufun/CommandStatus" {
		status := 0
		if atomic.AddInt64(&cmdPolls, 1) >= 3 {
			status = 1
		}
		_, _ = w.Write([]byte(fmt.Sprintf(`{"code":0,"msg":"ok","data":{"status":%d,"deviceName":"WeLockRKZQ7","description":"Unlocked"}}`, status)))
		return
	}
	data, ok := payloads[r.URL.Path]
	if !ok {
		data = "[]"
	}
	_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":` + data + `}`))
}

const tokens = `{"access_token":"mock-access-token-AAAA1111","refresh_token":"mock-refresh-token-BBBB2222"}`

// payloads maps each WeLock API path (POST, under /api) to a fake, anonymized `data` payload.
// Field names match exactly what the engine's bff parsers read.
var payloads = map[string]string{
	"/api/Account/token/welock":        tokens,
	"/api/Account/refresh/welock":      tokens,
	"/api/Account/MessageLogin/welock": tokens,

	"/api/Device/GetDevices": `[
		{"deviceNumber":"10002345","deviceName":"Front Door","type":1,"battery":87,"deviceModel":"TOUCAEBL51","ble":[{"deviceName":"WeLockRKZQ7","power":87,"position":0}]},
		{"deviceNumber":"70001122","deviceName":"Hallway Hub","type":6,"gatewayModel":"WIFIBOX3","gwType":"1"}
	]`,

	"/api/device/GetDeivceInfo": `{
		"deviceNumber":"10002345","deviceName":"Front Door","deviceModel":"TOUCAEBL51",
		"ble":[{"deviceName":"WeLockRKZQ7","power":87,"position":0},{"deviceName":"WeLockRKZQ7-2","power":72,"position":1}],
		"features":[
			{"id":1,"name":"Gateway PIN","features":"ble_gw_addPassword"},
			{"id":2,"name":"Gateway Card","features":"ble_gw_addCard"},
			{"id":3,"name":"BLE credentials","features":"mb_ble_addLocalPassword_mb_ble_addLocalCard_mb_ble_addLocalFp"}
		]
	}`,

	"/api/device/GetUnlockRecord": `[
		{"id":5001,"userID":"u-88213","nickName":"Alex Rivera","unlockingTime":"2026-07-25 08:31:20","position":"Keypad","unlockResult":1,"operatingState":2},
		{"id":5002,"userID":"u-77410","nickName":"Sam Chen","unlockingTime":"2026-07-24 19:02:10","openMode":"App","unlockResult":0,"operatingState":17},
		{"id":5003,"userID":"wl_9f2c8ab1d3e4","unlockingTime":"2026-07-24 07:45:00","position":"Fingerprint","unlockResult":1,"operatingState":2},
		{"id":5004,"userID":"u-51002","nickName":"Jordan Lee","unlockingTime":"2026-07-23 12:10:44","position":"Card","unlockResult":1,"operatingState":2}
	]`,

	"/api/device/TemporaryPasswordList": `[
		{"id":90271,"type":0,"password":"820394","startTime":1753432200,"endTime":1753468200,"time":"2026-07-25 08:30 to 18:30","remark":"Cleaner - Tuesday"},
		{"id":90272,"type":1,"password":"471065","startTime":1753603200,"endTime":1753606800,"time":"2026-07-27 08:00 to 09:00","remark":"Plumber (one-time)"}
	]`,

	"/api/device/DevicePasswordList2": `[
		{"user":"Alex Rivera","data":[{
			"Password":[{"number":"1001","user":"Alex Rivera","creationTime":"2026-06-01 10:00:00","endTime":0}],
			"Card":[{"number":"1005","user":"Alex Rivera","creationTime":"2026-06-02 09:30:00","endTime":0}]
		}]},
		{"user":"Sam Chen","data":[{
			"Password":[{"number":"1002","user":"Sam Chen","creationTime":"2026-06-05 14:20:00","endTime":1790000000}],
			"Fingerprint":[{"number":"1","user":"Sam Chen","creationTime":"2026-06-05 14:25:00","endTime":0}]
		}]},
		{"user":"Jordan Lee","data":[{
			"Password":[{"number":"1003","user":"Jordan Lee","creationTime":"2026-06-10 08:00:00","endTime":0}]
		}]}
	]`,

	"/api/device/PermissionList": `[
		{"account":"sam.chen@example.com","nickName":"Sam Chen","roleID":2,"beginTime":1748736000,"endTime":1790000000,"unlockNumber":0,"remark":"Family"},
		{"account":"jordan.lee@example.com","nickName":"Jordan Lee","roleID":3,"beginTime":1751328000,"endTime":1753920000,"unlockNumber":10,"remark":"Guest"}
	]`,

	"/api/ufun/GatewaysStatus": `[
		{"deviceNumber":"10002345","deviceName":"WeLockRKZQ7","battery":87,"signal":-63,"online":true,"status":"Online","description":"Front Door reachable via Hallway Hub"}
	]`,

	"/api/ufun/LockStatus": `{
		"status":"Online","gatewayModel":"WIFIBOX3","battery":87,"description":"Lock link healthy",
		"gateway":{"gateway":"70001122","gwType":"1","signal":-63,"type":1}
	}`,

	"/api/ufun/UnLock": `"cmd-mock-abc123"`,
}
