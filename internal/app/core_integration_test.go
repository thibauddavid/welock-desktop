package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestCoreSidecarRoundTrip exercises the whole app → embedded helper → cloud path against
// a mock WeLock server: spawn the helper (extract the embedded binary), init the session,
// import a token, run an authed Devices() call, and decode the reply into the local mirror
// types. It proves the sidecar boundary works without importing the engine.
func TestCoreSidecarRoundTrip(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/Device/GetDevices":
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":[
				{"deviceNumber":"10002345","deviceName":"Front Door","type":1,"battery":87}
			]}`))
		default:
			_, _ = w.Write([]byte(`{"code":0,"msg":"ok","data":[]}`))
		}
	}))
	defer srv.Close()

	// Isolate the session store so the real one is never touched.
	t.Setenv("HOME", t.TempDir())

	core, err := New(srv.URL + "/api")
	if err != nil {
		t.Fatalf("New (spawn helper): %v", err)
	}
	defer core.Close()

	// Constants are cached at New() — proves loadConstants (the helper health check) ran.
	if len(core.CredentialTypes()) == 0 {
		t.Fatal("CredentialTypes cache empty — helper did not answer")
	}
	if len(core.ValidityPresets()) == 0 {
		t.Fatal("ValidityPresets cache empty")
	}

	// A pure helper must resolve over the pipe.
	if msg := core.ValidatePin("TOUCAEBL51", "789012"); msg == "" {
		t.Fatal("ValidatePin should reject a PIN with 7/8/9 on a TOUCA lock")
	}

	ctx := context.Background()
	if err := core.LoginWithToken(ctx, "mock-access", "mock-refresh"); err != nil {
		t.Fatalf("LoginWithToken: %v", err)
	}
	if !core.LoggedIn() {
		t.Fatal("expected LoggedIn after token import")
	}

	devices, err := core.Devices(ctx)
	if err != nil {
		t.Fatalf("Devices: %v", err)
	}
	if len(devices) != 1 || devices[0].DeviceName != "Front Door" {
		t.Fatalf("Devices decoded wrong: %+v", devices)
	}
	if devices[0].Battery == nil || *devices[0].Battery != 87 {
		t.Fatalf("battery not decoded: %+v", devices[0])
	}
}
