package services

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/glebarez/sqlite"
	"gorm.io/datatypes"
	"gorm.io/gorm"
	"gpt-load/internal/channel"
	"gpt-load/internal/config"
	"gpt-load/internal/models"
	"gpt-load/internal/store"
)

type noPublishMemoryStore struct {
	*store.MemoryStore
}

func (s *noPublishMemoryStore) Publish(string, []byte) error { return nil }

func TestChannelConfigForUpdateClearsGenericConfigBeforeSwitch(t *testing.T) {
	current := datatypes.JSON(`{"version":1,"protocol":"http","auth":{"placement":"header","name":"Authorization","prefix":"Bearer "}}`)
	for _, supplied := range []json.RawMessage{nil, json.RawMessage(current)} {
		raw := channelConfigForUpdate(current, channel.GenericHTTPChannelType, "openai", supplied, supplied != nil)
		if string(raw) != "{}" {
			t.Fatalf("channel config after generic-to-openai switch = %s", raw)
		}
	}
}

func TestGenericAggregateSubGroupsRequireIdenticalNormalizedConfig(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "aggregate-config.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Group{}); err != nil {
		t.Fatal(err)
	}
	baseConfig, _ := json.Marshal(genericPresetByID(t, "tavily-mcp").ChannelConfig)
	different := genericPresetByID(t, "tavily-mcp").ChannelConfig
	different.StreamMode = channel.GenericStreamNever
	differentConfig, _ := json.Marshal(different)
	groups := []models.Group{
		{Name: "same-a", GroupType: "standard", ChannelType: channel.GenericHTTPChannelType, ChannelConfig: baseConfig, Upstreams: datatypes.JSON(`[]`), TestModel: "-"},
		{Name: "same-b", GroupType: "standard", ChannelType: channel.GenericHTTPChannelType, ChannelConfig: baseConfig, Upstreams: datatypes.JSON(`[]`), TestModel: "-"},
		{Name: "different", GroupType: "standard", ChannelType: channel.GenericHTTPChannelType, ChannelConfig: differentConfig, Upstreams: datatypes.JSON(`[]`), TestModel: "-"},
	}
	if err := db.Create(&groups).Error; err != nil {
		t.Fatal(err)
	}
	service := &AggregateGroupService{db: db}
	if _, err := service.ValidateSubGroups(context.Background(), channel.GenericHTTPChannelType, []SubGroupInput{{GroupID: groups[0].ID, Weight: 1}, {GroupID: groups[1].ID, Weight: 1}}, ""); err != nil {
		t.Fatalf("identical configs rejected: %v", err)
	}
	if _, err := service.ValidateSubGroups(context.Background(), channel.GenericHTTPChannelType, []SubGroupInput{{GroupID: groups[0].ID, Weight: 1}, {GroupID: groups[2].ID, Weight: 1}}, ""); err == nil {
		t.Fatal("different normalized configs were accepted")
	}
}

func TestValidateAndCleanGenericChannelConfig(t *testing.T) {
	preset := genericPresetByID(t, "tavily-http")
	raw, err := json.Marshal(preset.ChannelConfig)
	if err != nil {
		t.Fatal(err)
	}
	service := &GroupService{}
	cleaned, err := service.validateAndCleanChannelConfig(channel.GenericHTTPChannelType, "standard", raw)
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := channel.ParseGenericHTTPConfig(cleaned)
	if err != nil {
		t.Fatal(err)
	}
	if parsed.PresetID != "tavily-http" {
		t.Fatalf("preset ID = %q", parsed.PresetID)
	}

	if aggregateConfig, err := service.validateAndCleanChannelConfig(channel.GenericHTTPChannelType, "aggregate", []byte(`{}`)); err != nil || string(aggregateConfig) != "{}" {
		t.Fatalf("aggregate generic-http empty config = %s, %v", aggregateConfig, err)
	}
	if _, err := service.validateAndCleanChannelConfig(channel.GenericHTTPChannelType, "aggregate", raw); err == nil {
		t.Fatal("aggregate generic-http accepted a parent runtime config")
	}
	if _, err := service.validateAndCleanChannelConfig("openai", "standard", raw); err == nil {
		t.Fatal("standard channel accepted generic channel_config")
	}
}

func TestGenericGroupAcceptsLegacyAffinityRules(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(filepath.Join(t.TempDir(), "generic-affinity.db")), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.AutoMigrate(&models.Group{}, &models.GroupSubGroup{}); err != nil {
		t.Fatal(err)
	}
	memoryStore := &noPublishMemoryStore{MemoryStore: store.NewMemoryStore()}
	t.Cleanup(func() { _ = memoryStore.Close() })
	settingsManager := config.NewSystemSettingsManager()
	subGroupManager := NewSubGroupManager(memoryStore)
	groupManager := NewGroupManager(db, memoryStore, settingsManager, subGroupManager)
	if err := groupManager.Initialize(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { groupManager.Stop(context.Background()) })
	aggregateService := NewAggregateGroupService(db, groupManager)
	service := NewGroupService(db, settingsManager, groupManager, nil, nil, nil, aggregateService)
	preset := genericPresetByID(t, "tavily-mcp")
	channelConfig, _ := json.Marshal(preset.ChannelConfig)

	group, err := service.CreateGroup(context.Background(), GroupCreateParams{
		Name:          "generic-with-legacy-affinity",
		GroupType:     "standard",
		ChannelType:   channel.GenericHTTPChannelType,
		Upstreams:     json.RawMessage(`[{"url":"https://mcp.tavily.com","weight":1}]`),
		ChannelConfig: channelConfig,
		AffinityRules: []models.AffinityRule{{
			Name:      "session key affinity",
			Match:     models.AffinityMatchRule{PathRegex: `.*`},
			KeySource: models.AffinityKeySource{Type: "header", Key: "X-Legacy-Session"},
			TTL:       3600,
		}},
	})
	if err != nil {
		t.Fatalf("generic group rejected legacy affinity rules: %v", err)
	}
	if len(group.AffinityRules) == 0 || string(group.AffinityRules) == "[]" {
		t.Fatalf("legacy affinity rules were not persisted: %s", group.AffinityRules)
	}
}

func TestGenericHeaderRuleProtectionIsFixedAndDynamic(t *testing.T) {
	service := &GroupService{}
	if _, err := service.normalizeHeaderRules([]models.HeaderRule{{Key: "Last-Event-ID", Action: "remove"}}); err == nil {
		t.Fatal("reserved Last-Event-ID header rule was accepted")
	}

	cfg := genericPresetByID(t, "tavily-mcp").ChannelConfig
	for _, name := range []string{cfg.Auth.Name} {
		err := validateGenericHeaderRules([]models.HeaderRule{{Key: strings.ToLower(name), Value: "attacker", Action: "set"}}, cfg)
		if err == nil {
			t.Fatalf("proxy-managed header %q was accepted", name)
		}
	}
	if err := validateGenericHeaderRules([]models.HeaderRule{{Key: "X-Custom", Value: "ok", Action: "set"}}, cfg); err != nil {
		t.Fatalf("ordinary custom header was rejected: %v", err)
	}
	for _, rule := range []models.HeaderRule{
		{Key: "Bad Header", Value: "value", Action: "set"},
		{Key: "X-Custom", Value: "value", Action: "append"},
		{Key: "X-Custom", Value: "line\r\nbreak", Action: "set"},
	} {
		if _, err := service.normalizeHeaderRules([]models.HeaderRule{rule}); err == nil {
			t.Fatalf("invalid header rule was accepted: %#v", rule)
		}
		if err := validateGenericHeaderRules([]models.HeaderRule{rule}, cfg); err == nil {
			t.Fatalf("invalid persisted generic header rule was accepted: %#v", rule)
		}
	}
	normalized, err := service.normalizeHeaderRules([]models.HeaderRule{{Key: "X-Remove", Value: "stale", Action: " REMOVE "}})
	if err != nil {
		t.Fatal(err)
	}
	var removeRules []models.HeaderRule
	if err := json.Unmarshal(normalized, &removeRules); err != nil {
		t.Fatal(err)
	}
	if len(removeRules) != 1 || removeRules[0].Action != "remove" || removeRules[0].Value != "" {
		t.Fatalf("remove rule was not normalized: %#v", removeRules)
	}
}

func genericPresetByID(t *testing.T, id string) channel.ChannelCatalogEntry {
	t.Helper()
	for _, preset := range channel.GetChannelCatalog() {
		if preset.ID == id {
			return preset
		}
	}
	t.Fatalf("channel preset %q not found", id)
	return channel.ChannelCatalogEntry{}
}

func TestValidateAndCleanGenericUpstreamsIsStrict(t *testing.T) {
	service := &GroupService{}
	valid := []byte(`[{"url":"https://one.example/base","weight":1},{"url":"http://two.example","weight":2}]`)
	if _, err := service.validateAndCleanUpstreams(valid, channel.GenericHTTPChannelType); err != nil {
		t.Fatal(err)
	}
	const secretURL = "https://url-secret@example.test/path"
	for _, raw := range []string{
		`[{"url":"/relative","weight":1}]`,
		`[{"url":"https:///missing-host","weight":1}]`,
		`[{"url":"` + secretURL + `","weight":1}]`,
		`[{"url":"https://example.test/path#fragment","weight":1}]`,
		`[{"url":"https://example.test/path?region=us","weight":1}]`,
		`[{"url":"https://example.test/path?api_key=secret","weight":1}]`,
	} {
		if _, err := service.validateAndCleanUpstreams([]byte(raw), channel.GenericHTTPChannelType); err == nil {
			t.Fatalf("invalid generic upstream accepted: %s", raw)
		} else if strings.Contains(err.Error(), secretURL) {
			t.Fatalf("upstream validation error leaked full URL: %v", err)
		}
	}
}
