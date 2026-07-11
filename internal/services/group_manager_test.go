package services

import (
	"fmt"
	"strings"
	"testing"

	"gpt-load/internal/models"
	"gpt-load/internal/types"
)

func TestGroupConfigSummaryFieldsExcludeCredentials(t *testing.T) {
	const proxySecret = "proxy-key-that-must-not-be-logged"
	const proxyPassword = "proxy-password-that-must-not-be-logged"
	group := &models.Group{
		Name: "primary",
		EffectiveConfig: types.SystemSettings{
			ProxyKeys:    proxySecret,
			ProxyKeysMap: map[string]struct{}{proxySecret: {}},
			ProxyURL:     "http://operator:" + proxyPassword + "@proxy.example.test",
			MaxRetries:   3,
		},
	}

	fields := groupConfigSummaryFields(group)
	rendered := fmt.Sprint(fields)
	for _, secret := range []string{proxySecret, proxyPassword} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("group config summary leaked %q: %s", secret, rendered)
		}
	}
	if got := fields["proxy_key_count"]; got != 1 {
		t.Fatalf("proxy_key_count = %v, want 1", got)
	}
}
