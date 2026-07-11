package channel

import (
	"encoding/json"
	"testing"
)

func TestHostedMCPPresetsUsePlainGenericHTTPConfig(t *testing.T) {
	allowedFields := map[string]struct{}{
		"version": {}, "preset_id": {}, "auth": {}, "validation": {},
		"stream_mode": {}, "retry": {}, "max_request_body_bytes": {}, "max_error_body_bytes": {},
	}
	wanted := map[string]bool{"tavily-mcp": false, "exa-mcp": false}
	for _, entry := range GetChannelCatalog() {
		if _, ok := wanted[entry.ID]; !ok {
			continue
		}
		wanted[entry.ID] = true
		if entry.ChannelType != GenericHTTPChannelType || entry.IntegrationKind != "hosted_mcp" {
			t.Fatalf("preset %s catalog identity = %s/%s", entry.ID, entry.ChannelType, entry.IntegrationKind)
		}
		if entry.ChannelConfig.Auth.Placement != GenericAuthHeader || !entry.ChannelConfig.Validation.Enabled || entry.ChannelConfig.StreamMode != GenericStreamAuto {
			t.Fatalf("preset %s is not plain header-auth HTTP config: %#v", entry.ID, entry.ChannelConfig)
		}
		if got := entry.ChannelConfig.Retry.SafeMethods; len(got) != 2 || got[0] != "GET" || got[1] != "HEAD" {
			t.Fatalf("preset %s safe methods = %#v", entry.ID, got)
		}

		raw, err := json.Marshal(entry.ChannelConfig)
		if err != nil {
			t.Fatal(err)
		}
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(raw, &fields); err != nil {
			t.Fatal(err)
		}
		if len(fields) != len(allowedFields) {
			t.Fatalf("preset %s config fields = %#v", entry.ID, fields)
		}
		for name := range fields {
			if _, ok := allowedFields[name]; !ok {
				t.Fatalf("preset %s exposes non-transparent field %q", entry.ID, name)
			}
		}
		if _, _, err := NormalizeGenericHTTPConfig(raw); err != nil {
			t.Fatalf("preset %s is not normalized by the ordinary parser: %v", entry.ID, err)
		}
	}
	for id, found := range wanted {
		if !found {
			t.Fatalf("hosted MCP preset %s is missing", id)
		}
	}
}
