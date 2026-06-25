package errorpolicy

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

type RequestAction string
type HealthAction string

const (
	RequestActionReturn        RequestAction = "return"
	RequestActionRetryOtherKey RequestAction = "retry_other_key"

	HealthActionNoop         HealthAction = "noop"
	HealthActionFailCountInc HealthAction = "fail_count_inc"
	HealthActionCooldown     HealthAction = "cooldown"
	HealthActionBlacklistNow HealthAction = "blacklist_now"
)

const DefaultCooldownSeconds = 60

type Match struct {
	Status []int `json:"status"`
}

type Params struct {
	CooldownSeconds int `json:"cooldown_seconds,omitempty"`
}

type Decision struct {
	OnRequest RequestAction `json:"on_request"`
	Health    HealthAction  `json:"health"`
	Params    Params        `json:"params,omitempty"`
}

type Rule struct {
	Match     Match         `json:"match"`
	OnRequest RequestAction `json:"on_request"`
	Health    HealthAction  `json:"health"`
	Params    Params        `json:"params,omitempty"`
}

type Policy struct {
	Rules   []Rule    `json:"rules"`
	Default *Decision `json:"default,omitempty"`
}

func DefaultPolicy() Policy {
	return Policy{
		Rules: []Rule{
			{
				Match:     Match{Status: []int{400, 404}},
				OnRequest: RequestActionReturn,
				Health:    HealthActionNoop,
			},
			{
				Match:     Match{Status: []int{401, 403}},
				OnRequest: RequestActionReturn,
				Health:    HealthActionBlacklistNow,
			},
			{
				Match:     Match{Status: []int{408, 500, 502, 504}},
				OnRequest: RequestActionRetryOtherKey,
				Health:    HealthActionFailCountInc,
			},
			{
				Match:     Match{Status: []int{429, 503, 529}},
				OnRequest: RequestActionRetryOtherKey,
				Health:    HealthActionCooldown,
				Params:    Params{CooldownSeconds: DefaultCooldownSeconds},
			},
		},
		Default: &Decision{
			OnRequest: RequestActionRetryOtherKey,
			Health:    HealthActionFailCountInc,
		},
	}
}

func DefaultPolicyJSON() string {
	encoded, err := json.MarshalIndent(DefaultPolicy(), "", "  ")
	if err != nil {
		return ""
	}
	return string(encoded)
}

func Parse(raw string) (Policy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Policy{}, fmt.Errorf("error policy cannot be empty")
	}

	var policy Policy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return Policy{}, fmt.Errorf("invalid error policy JSON: %w", err)
	}
	if err := Validate(policy, true); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func ParseOverride(raw string) (Policy, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return Policy{}, nil
	}

	var policy Policy
	if err := json.Unmarshal([]byte(raw), &policy); err != nil {
		return Policy{}, fmt.Errorf("invalid error policy JSON: %w", err)
	}
	if err := Validate(policy, false); err != nil {
		return Policy{}, err
	}
	return policy, nil
}

func NormalizeJSON(policy Policy) (string, error) {
	if err := Validate(policy, true); err != nil {
		return "", err
	}
	encoded, err := json.MarshalIndent(policy, "", "  ")
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

func MergeJSON(baseRaw, overrideRaw string) (string, error) {
	base, err := Parse(baseRaw)
	if err != nil {
		return "", err
	}

	override, err := ParseOverride(overrideRaw)
	if err != nil {
		return "", err
	}

	merged := Merge(base, override)
	return NormalizeJSON(merged)
}

func Merge(base, override Policy) Policy {
	defaultDecision := fallbackDefault(base.Default)
	if override.Default != nil {
		defaultDecision = fallbackDefault(override.Default)
	}

	byStatus := make(map[int]Decision)
	for _, rule := range base.Rules {
		decision := rule.Decision()
		for _, status := range rule.Match.Status {
			byStatus[status] = decision
		}
	}
	for _, rule := range override.Rules {
		decision := rule.Decision()
		for _, status := range rule.Match.Status {
			byStatus[status] = decision
		}
	}

	statuses := make([]int, 0, len(byStatus))
	for status := range byStatus {
		statuses = append(statuses, status)
	}
	sort.Ints(statuses)

	rules := make([]Rule, 0, len(statuses))
	for _, status := range statuses {
		decision := byStatus[status]
		rules = append(rules, Rule{
			Match:     Match{Status: []int{status}},
			OnRequest: decision.OnRequest,
			Health:    decision.Health,
			Params:    decision.Params,
		})
	}

	return Policy{
		Rules:   rules,
		Default: &defaultDecision,
	}
}

func (p Policy) Decide(statusCode int) Decision {
	for _, rule := range p.Rules {
		for _, status := range rule.Match.Status {
			if status == statusCode {
				return rule.Decision()
			}
		}
	}
	return fallbackDefault(p.Default)
}

func (r Rule) Decision() Decision {
	return Decision{
		OnRequest: r.OnRequest,
		Health:    r.Health,
		Params:    r.Params,
	}
}

func Validate(policy Policy, requireDefault bool) error {
	if requireDefault && policy.Default == nil {
		return fmt.Errorf("error policy default is required")
	}
	if policy.Default != nil {
		if err := validateDecision(*policy.Default, "default"); err != nil {
			return err
		}
	}

	seen := make(map[int]struct{})
	for i, rule := range policy.Rules {
		prefix := "rules[" + strconv.Itoa(i) + "]"
		if len(rule.Match.Status) == 0 {
			return fmt.Errorf("%s.match.status must not be empty", prefix)
		}
		if err := validateDecision(rule.Decision(), prefix); err != nil {
			return err
		}
		for _, status := range rule.Match.Status {
			if status < 100 || status > 999 {
				return fmt.Errorf("%s.match.status contains invalid status %d", prefix, status)
			}
			if _, ok := seen[status]; ok {
				return fmt.Errorf("status %d is configured more than once", status)
			}
			seen[status] = struct{}{}
		}
	}

	return nil
}

func validateDecision(decision Decision, prefix string) error {
	switch decision.OnRequest {
	case RequestActionReturn, RequestActionRetryOtherKey:
	default:
		return fmt.Errorf("%s.on_request has unsupported value %q", prefix, decision.OnRequest)
	}

	switch decision.Health {
	case HealthActionNoop, HealthActionFailCountInc, HealthActionCooldown, HealthActionBlacklistNow:
	default:
		return fmt.Errorf("%s.health has unsupported value %q", prefix, decision.Health)
	}

	if decision.Params.CooldownSeconds < 0 {
		return fmt.Errorf("%s.params.cooldown_seconds must not be negative", prefix)
	}

	return nil
}

func fallbackDefault(defaultDecision *Decision) Decision {
	if defaultDecision == nil {
		return Decision{
			OnRequest: RequestActionRetryOtherKey,
			Health:    HealthActionFailCountInc,
		}
	}

	decision := *defaultDecision
	if decision.OnRequest == "" {
		decision.OnRequest = RequestActionRetryOtherKey
	}
	if decision.Health == "" {
		decision.Health = HealthActionFailCountInc
	}
	return decision
}
