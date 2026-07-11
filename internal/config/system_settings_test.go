package config

import (
	"bytes"
	"gpt-load/internal/errorpolicy"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
	"gorm.io/datatypes"
)

func TestGetEffectiveConfigMergesGroupErrorPolicy(t *testing.T) {
	sm := NewSystemSettingsManager()
	effective := sm.GetEffectiveConfig(datatypes.JSONMap{
		"error_policy": `{
		  "rules": [
		    {
		      "match": { "status": [401] },
		      "on_request": "retry_other_key",
		      "health": "fail_count_inc"
		    }
		  ],
		  "default": {
		    "on_request": "return",
		    "health": "noop"
		  }
		}`,
	})

	policy, err := errorpolicy.Parse(effective.ErrorPolicy)
	if err != nil {
		t.Fatalf("effective error policy failed to parse: %v", err)
	}

	if decision := policy.Decide(400); decision.OnRequest != errorpolicy.RequestActionReturn || decision.Health != errorpolicy.HealthActionNoop {
		t.Fatalf("400 decision = %+v, want base return + noop", decision)
	}
	if decision := policy.Decide(401); decision.OnRequest != errorpolicy.RequestActionRetryOtherKey || decision.Health != errorpolicy.HealthActionFailCountInc {
		t.Fatalf("401 decision = %+v, want group override retry_other_key + fail_count_inc", decision)
	}
	if decision := policy.Decide(599); decision.OnRequest != errorpolicy.RequestActionReturn || decision.Health != errorpolicy.HealthActionNoop {
		t.Fatalf("default decision = %+v, want group default return + noop", decision)
	}
}

func TestGetEffectiveConfigKeepsSystemErrorPolicyForEmptyGroupOverride(t *testing.T) {
	sm := NewSystemSettingsManager()
	effective := sm.GetEffectiveConfig(datatypes.JSONMap{
		"error_policy": `{"rules":[]}`,
	})

	policy, err := errorpolicy.Parse(effective.ErrorPolicy)
	if err != nil {
		t.Fatalf("effective error policy failed to parse: %v", err)
	}

	if decision := policy.Decide(401); decision.OnRequest != errorpolicy.RequestActionReturn || decision.Health != errorpolicy.HealthActionBlacklistNow {
		t.Fatalf("401 decision = %+v, want system default return + blacklist_now", decision)
	}
	if decision := policy.Decide(599); decision.OnRequest != errorpolicy.RequestActionRetryOtherKey || decision.Health != errorpolicy.HealthActionFailCountInc {
		t.Fatalf("default decision = %+v, want system default retry_other_key + fail_count_inc", decision)
	}
}

func TestValidateGroupConfigOverridesKeepsLegacyFailoverCompatibility(t *testing.T) {
	sm := NewSystemSettingsManager()

	if err := sm.ValidateGroupConfigOverrides(map[string]any{
		"failover_status_codes": "400-403,405-999",
		"error_policy":          `{"rules":[]}`,
	}); err != nil {
		t.Fatalf("ValidateGroupConfigOverrides returned error: %v", err)
	}
}

func TestLogInitializedSystemSettingRedactsSensitiveValue(t *testing.T) {
	var output bytes.Buffer
	oldOutput := logrus.StandardLogger().Out
	oldFormatter := logrus.StandardLogger().Formatter
	logrus.SetOutput(&output)
	logrus.SetFormatter(&logrus.TextFormatter{DisableTimestamp: true, DisableColors: true})
	t.Cleanup(func() {
		logrus.SetOutput(oldOutput)
		logrus.SetFormatter(oldFormatter)
	})

	const secret = "auth-setting-secret"
	logInitializedSystemSetting("AUTH_KEY", secret)
	if strings.Contains(output.String(), secret) {
		t.Fatalf("sensitive system setting leaked to log: %s", output.String())
	}
	if !strings.Contains(output.String(), "AUTH_KEY") || !strings.Contains(output.String(), "value redacted") {
		t.Fatalf("redacted log lost safe setting context: %s", output.String())
	}
}
