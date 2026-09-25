package httpclient

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func TestDynamicProxyAuthenticationUsesRequestIdentity(t *testing.T) {
	const proxyPassword = "private-proxy-token"
	var usernames []string
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.Method != http.MethodConnect {
			t.Errorf("method = %q, want CONNECT", req.Method)
		}
		encoded := strings.TrimPrefix(req.Header.Get("Proxy-Authorization"), "Basic ")
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			t.Errorf("decode proxy auth: %v", err)
		}
		username, password, found := strings.Cut(string(decoded), ":")
		if !found || password != proxyPassword {
			t.Errorf("proxy authentication password invalid")
		}
		usernames = append(usernames, username)
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer proxy.Close()

	proxyHost := strings.TrimPrefix(proxy.URL, "http://")
	client := NewHTTPClientManager().GetClient(&Config{
		ProxyURL:       fmt.Sprintf("http://Default.${API_KEY_FINGERPRINT}:%s@%s", proxyPassword, proxyHost),
		ConnectTimeout: 2 * time.Second,
		RequestTimeout: 2 * time.Second,
	})

	for _, identity := range []string{"fp:111111111111", "fp:222222222222"} {
		ctx := WithProxyIdentity(context.Background(), identity)
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://upstream.example.test/v1", nil)
		if err != nil {
			t.Fatal(err)
		}
		_, err = client.Do(req)
		if err == nil {
			t.Fatal("CONNECT rejection unexpectedly succeeded")
		}
		if strings.Contains(err.Error(), proxyPassword) {
			t.Fatal("proxy password leaked in transport error")
		}
	}

	if len(usernames) != 2 || usernames[0] != "Default.fp-111111111111" || usernames[1] != "Default.fp-222222222222" {
		t.Errorf("proxy usernames = %q", usernames)
	}
}

func TestDynamicProxyIdentityMissingFailsClosed(t *testing.T) {
	proxyURL := "http://Default.${API_KEY_FINGERPRINT}:private-token@proxy.example.test"
	selectProxy := proxyForConfig(proxyURL)
	req := httptest.NewRequest(http.MethodGet, "https://upstream.example.test", nil)
	parsed, err := selectProxy(req)
	if err == nil || parsed != nil {
		t.Fatalf("missing identity selected proxy %v, err %v", parsed, err)
	}
	if strings.Contains(err.Error(), "private-token") {
		t.Fatal("proxy password leaked in error")
	}
}

func TestDynamicProxyVariableOnlyAllowedInUsername(t *testing.T) {
	for _, raw := range []string{
		"http://Default:token@${API_KEY_FINGERPRINT}.example.test",
		"http://Default:${API_KEY_FINGERPRINT}@proxy.example.test",
		"http://Default.${API_KEY_FINGERPRINT}.${API_KEY_FINGERPRINT}:token@proxy.example.test",
	} {
		selectProxy := proxyForConfig(raw)
		req := httptest.NewRequest(http.MethodGet, "https://upstream.example.test", nil)
		req = req.WithContext(WithProxyIdentity(req.Context(), "fp:123456789abc"))
		parsed, err := selectProxy(req)
		if err == nil || parsed != nil {
			t.Errorf("invalid template %q selected proxy %v, err %v", url.PathEscape(raw), parsed, err)
		}
	}
}
