package proxy

import (
	"gpt-load/internal/errorpolicy"
	"net/http"
	"testing"
	"time"
)

func TestCooldownDurationUsesRetryAfterSeconds(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "3600")

	got := cooldownDuration(cooldownDecision(45), resp)
	if got != time.Hour {
		t.Fatalf("cooldownDuration = %v, want 1h", got)
	}
}

func TestCooldownDurationUsesRetryAfterDate(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", time.Now().Add(2*time.Minute).UTC().Format(http.TimeFormat))

	got := cooldownDuration(cooldownDecision(45), resp)
	if got < 110*time.Second || got > 121*time.Second {
		t.Fatalf("cooldownDuration = %v, want approximately 2m", got)
	}
}

func TestCooldownDurationCapsRetryAfter(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "9999999")

	got := cooldownDuration(cooldownDecision(45), resp)
	if got != maxRetryAfterCooldown {
		t.Fatalf("cooldownDuration = %v, want max retry-after cap", got)
	}
}

func TestCooldownDurationFallsBackToPolicyParamWhenRetryAfterInvalid(t *testing.T) {
	resp := &http.Response{Header: http.Header{}}
	resp.Header.Set("Retry-After", "not-a-duration")

	got := cooldownDuration(cooldownDecision(45), resp)
	if got != 45*time.Second {
		t.Fatalf("cooldownDuration = %v, want 45s", got)
	}
}

func TestCooldownDurationUsesPolicyParam(t *testing.T) {
	got := cooldownDuration(cooldownDecision(45), nil)
	if got != 45*time.Second {
		t.Fatalf("cooldownDuration = %v, want 45s", got)
	}
}

func TestCooldownDurationFallsBackToDefault(t *testing.T) {
	got := cooldownDuration(cooldownDecision(0), nil)
	if got != time.Duration(errorpolicy.DefaultCooldownSeconds)*time.Second {
		t.Fatalf("cooldownDuration = %v, want default cooldown", got)
	}
}

func TestCooldownDurationIgnoresNonCooldownHealth(t *testing.T) {
	got := cooldownDuration(errorpolicy.Decision{
		OnRequest: errorpolicy.RequestActionRetryOtherKey,
		Health:    errorpolicy.HealthActionFailCountInc,
	}, nil)
	if got != 0 {
		t.Fatalf("cooldownDuration = %v, want 0", got)
	}
}

func cooldownDecision(seconds int) errorpolicy.Decision {
	return errorpolicy.Decision{
		OnRequest: errorpolicy.RequestActionRetryOtherKey,
		Health:    errorpolicy.HealthActionCooldown,
		Params:    errorpolicy.Params{CooldownSeconds: seconds},
	}
}
