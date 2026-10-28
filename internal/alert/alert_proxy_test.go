package alert

import (
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
)

// resetSharedHTTPClient rebuilds the shared client on next use, picking up
// the current alertProxy.
func resetSharedHTTPClient(t *testing.T) {
	t.Helper()
	sharedHTTPClient.once = sync.Once{}
	sharedHTTPClient.c = nil
	t.Cleanup(func() {
		alertProxy = nil
		sharedHTTPClient.once = sync.Once{}
		sharedHTTPClient.c = nil
	})
}

// With alerting.proxy set, alert HTTP requests go to the proxy (as an
// absolute-URI request) instead of dialing the target directly.
func TestAlertingProxyRoutesThroughProxy(t *testing.T) {
	var seen atomic.Value
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen.Store(r.URL.String())
		w.WriteHeader(http.StatusNoContent)
	}))
	defer proxy.Close()

	if err := ConfigureHTTPProxy(proxy.URL); err != nil {
		t.Fatal(err)
	}
	resetSharedHTTPClient(t)

	resp, err := getHTTPClient().Get("http://alerts.example.com/hook")
	if err != nil {
		t.Fatalf("send via proxy: %v", err)
	}
	resp.Body.Close()
	if got, _ := seen.Load().(string); got != "http://alerts.example.com/hook" {
		t.Fatalf("proxy saw %q, want the absolute target URL", got)
	}
}

func TestConfigureHTTPProxyRejectsBadURLs(t *testing.T) {
	t.Cleanup(func() { alertProxy = nil })
	for _, raw := range []string{"socks5://proxy:1080", "proxy:3128", "http://"} {
		if err := ConfigureHTTPProxy(raw); err == nil {
			t.Errorf("ConfigureHTTPProxy(%q) accepted", raw)
		}
	}
	if err := ConfigureHTTPProxy(""); err != nil || alertProxy != nil {
		t.Errorf("empty proxy: err=%v proxy=%v", err, alertProxy)
	}
}
