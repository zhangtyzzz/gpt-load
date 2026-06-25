package errorpolicy

import "testing"

func TestDefaultPolicyDecisions(t *testing.T) {
	policy := DefaultPolicy()

	tests := []struct {
		status   int
		request  RequestAction
		health   HealthAction
		cooldown int
	}{
		{status: 400, request: RequestActionReturn, health: HealthActionNoop},
		{status: 401, request: RequestActionReturn, health: HealthActionBlacklistNow},
		{status: 429, request: RequestActionRetryOtherKey, health: HealthActionCooldown, cooldown: DefaultCooldownSeconds},
		{status: 529, request: RequestActionRetryOtherKey, health: HealthActionCooldown, cooldown: DefaultCooldownSeconds},
		{status: 599, request: RequestActionRetryOtherKey, health: HealthActionFailCountInc},
	}

	for _, tt := range tests {
		decision := policy.Decide(tt.status)
		if decision.OnRequest != tt.request {
			t.Fatalf("status %d request action = %s, want %s", tt.status, decision.OnRequest, tt.request)
		}
		if decision.Health != tt.health {
			t.Fatalf("status %d health action = %s, want %s", tt.status, decision.Health, tt.health)
		}
		if tt.cooldown > 0 && decision.Params.CooldownSeconds != tt.cooldown {
			t.Fatalf("status %d cooldown = %d, want %d", tt.status, decision.Params.CooldownSeconds, tt.cooldown)
		}
	}
}

func TestParsePolicySupportsAllStrategyCombinations(t *testing.T) {
	policy, err := Parse(`{
	  "rules": [
	    {
	      "match": { "status": [418] },
	      "on_request": "return",
	      "health": "noop"
	    },
	    {
	      "match": { "status": [419] },
	      "on_request": "return",
	      "health": "blacklist_now"
	    },
	    {
	      "match": { "status": [420] },
	      "on_request": "retry_other_key",
	      "health": "fail_count_inc"
	    },
	    {
	      "match": { "status": [421] },
	      "on_request": "retry_other_key",
	      "health": "cooldown",
	      "params": { "cooldown_seconds": 45 }
	    }
	  ],
	  "default": {
	    "on_request": "retry_other_key",
	    "health": "fail_count_inc"
	  }
	}`)
	if err != nil {
		t.Fatalf("Parse returned error: %v", err)
	}

	tests := []struct {
		status   int
		request  RequestAction
		health   HealthAction
		cooldown int
	}{
		{status: 418, request: RequestActionReturn, health: HealthActionNoop},
		{status: 419, request: RequestActionReturn, health: HealthActionBlacklistNow},
		{status: 420, request: RequestActionRetryOtherKey, health: HealthActionFailCountInc},
		{status: 421, request: RequestActionRetryOtherKey, health: HealthActionCooldown, cooldown: 45},
		{status: 599, request: RequestActionRetryOtherKey, health: HealthActionFailCountInc},
	}

	for _, tt := range tests {
		decision := policy.Decide(tt.status)
		if decision.OnRequest != tt.request {
			t.Fatalf("status %d request action = %s, want %s", tt.status, decision.OnRequest, tt.request)
		}
		if decision.Health != tt.health {
			t.Fatalf("status %d health action = %s, want %s", tt.status, decision.Health, tt.health)
		}
		if tt.cooldown > 0 && decision.Params.CooldownSeconds != tt.cooldown {
			t.Fatalf("status %d cooldown = %d, want %d", tt.status, decision.Params.CooldownSeconds, tt.cooldown)
		}
	}
}

func TestMergeOverridesStatusAndDefault(t *testing.T) {
	base := DefaultPolicy()
	override, err := ParseOverride(`{
	  "rules": [
	    {
	      "match": { "status": [401] },
	      "on_request": "retry_other_key",
	      "health": "fail_count_inc"
	    },
	    {
	      "match": { "status": [418] },
	      "on_request": "return",
	      "health": "noop"
	    }
	  ],
	  "default": {
	    "on_request": "return",
	    "health": "noop"
	  }
	}`)
	if err != nil {
		t.Fatalf("ParseOverride returned error: %v", err)
	}

	merged := Merge(base, override)

	if decision := merged.Decide(401); decision.OnRequest != RequestActionRetryOtherKey || decision.Health != HealthActionFailCountInc {
		t.Fatalf("401 decision = %+v, want retry_other_key + fail_count_inc", decision)
	}
	if decision := merged.Decide(418); decision.OnRequest != RequestActionReturn || decision.Health != HealthActionNoop {
		t.Fatalf("418 decision = %+v, want return + noop", decision)
	}
	if decision := merged.Decide(599); decision.OnRequest != RequestActionReturn || decision.Health != HealthActionNoop {
		t.Fatalf("default decision = %+v, want return + noop", decision)
	}
}

func TestValidateRejectsDuplicateStatus(t *testing.T) {
	_, err := Parse(`{
	  "rules": [
	    {
	      "match": { "status": [429] },
	      "on_request": "retry_other_key",
	      "health": "cooldown"
	    },
	    {
	      "match": { "status": [429] },
	      "on_request": "return",
	      "health": "noop"
	    }
	  ],
	  "default": {
	    "on_request": "retry_other_key",
	    "health": "fail_count_inc"
	  }
	}`)
	if err == nil {
		t.Fatal("expected duplicate status validation error")
	}
}
