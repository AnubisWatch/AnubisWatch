package core

import (
	"strings"
	"testing"
)

func TestAlertingProxyValidation(t *testing.T) {
	tests := []struct {
		proxy string
		ok    bool
	}{
		{"", true},
		{"http://proxy.corp:3128", true},
		{"https://proxy.corp", true},
		{"socks5://proxy.corp:1080", false},
		{"proxy.corp:3128", false},
		{"http://", false},
	}
	for _, tt := range tests {
		err := AlertingConfig{Proxy: tt.proxy}.validate()
		if (err == nil) != tt.ok {
			t.Errorf("proxy %q: err=%v, want ok=%v", tt.proxy, err, tt.ok)
		}
		if err != nil && !strings.Contains(err.Error(), "alerting.proxy") {
			t.Errorf("proxy %q: error %q does not name alerting.proxy", tt.proxy, err)
		}
	}
}
