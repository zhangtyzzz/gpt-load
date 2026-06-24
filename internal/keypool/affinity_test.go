package keypool

import (
	"gpt-load/internal/models"
	"gpt-load/internal/store"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

func TestComputeAffinityHash(t *testing.T) {
	hash1 := ComputeAffinityHash("test-user-123")
	hash2 := ComputeAffinityHash("test-user-123")
	hash3 := ComputeAffinityHash("test-user-456")

	if hash1 == "" {
		t.Error("Hash should not be empty")
	}
	if hash1 != hash2 {
		t.Errorf("Same input should produce same hash, got %s and %s", hash1, hash2)
	}
	if hash1 == hash3 {
		t.Error("Different inputs should produce different hashes")
	}
	if len(hash1) != 64 { // SHA256 hex = 64 chars
		t.Errorf("Expected hash length 64, got %d", len(hash1))
	}
}

func TestExtractJSONPath(t *testing.T) {
	tests := []struct {
		name     string
		json     string
		path     string
		expected string
	}{
		{
			name:     "simple field",
			json:     `{"user": "alice"}`,
			path:     "user",
			expected: "alice",
		},
		{
			name:     "nested field",
			json:     `{"metadata": {"user_id": "12345"}}`,
			path:     "metadata.user_id",
			expected: "12345",
			},
		{
			name:     "missing field",
			json:     `{"user": "alice"}`,
			path:     "nonexistent",
			expected: "",
		},
		{
			name:     "empty json",
			json:     `{}`,
			path:     "user",
			expected: "",
		},
		{
			name:     "numeric value",
			json:     `{"count": 42}`,
			path:     "count",
			expected: "42",
		},
		{
			name:     "array index",
			json:     `{"items": ["a", "b", "c"]}`,
			path:     "items.1",
			expected: "b",
		},
		{
			name:     "deeply nested",
			json:     `{"a": {"b": {"c": "deep"}}}`,
			path:     "a.b.c",
			expected: "deep",
		},
		{
			name:     "null value",
			json:     `{"user": null}`,
			path:     "user",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractJSONPath([]byte(tt.json), tt.path)
			if result != tt.expected {
				t.Errorf("extractJSONPath(%q, %q) = %q, want %q", tt.json, tt.path, result, tt.expected)
			}
		})
	}
}

func TestExtractByBodyRegex(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		pattern  string
		expected string
	}{
		{
			name:     "named capture group",
			body:     `{"prompt_cache_key": "cache-abc-123"}`,
			pattern:  `"prompt_cache_key"\s*:\s*"(?P<value>[^"]+)"`,
			expected: "cache-abc-123",
		},
		{
			name:     "simple capture group",
			body:     `user_id: 12345`,
			pattern:  `user_id:\s*(\d+)`,
			expected: "12345",
		},
		{
			name:     "no match",
			body:     `{"other": "data"}`,
			pattern:  `"user"\s*:\s*"(?P<value>[^"]+)"`,
			expected: "",
		},
		{
			name:     "invalid regex",
			body:     `test`,
			pattern:  `[invalid`,
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := extractByBodyRegex([]byte(tt.body), tt.pattern)
			if result != tt.expected {
				t.Errorf("extractByBodyRegex(%q, %q) = %q, want %q", tt.body, tt.pattern, result, tt.expected)
			}
		})
	}
}

func TestExtractValue(t *testing.T) {
	// Create a mock store
	memStore := store.NewMemoryStore()
	am := NewAffinityManager(memStore)

	// Setup gin context
	gin.SetMode(gin.TestMode)

	t.Run("header extraction", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		c.Request.Header.Set("X-Session-Id", "session-abc-123")

		rules := []models.AffinityRule{
			{
				Name: "session-affinity",
				Match: models.AffinityMatchRule{
					PathRegex: ".*",
				},
				KeySource: models.AffinityKeySource{
					Type: "header",
					Key:  "X-Session-Id",
				},
			},
		}

		result := am.ExtractValue(c, nil, "gpt-4", rules)
		if result.Value != "session-abc-123" {
			t.Errorf("Expected 'session-abc-123', got '%s'", result.Value)
		}
		if result.Hash == "" {
			t.Error("Expected non-empty hash")
		}
		if result.MatchedRule == nil || result.MatchedRule.Name != "session-affinity" {
			t.Error("Expected matched rule 'session-affinity'")
		}
	})

	t.Run("body_json extraction", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

		body := []byte(`{"model": "gpt-4", "user": "user-123"}`)
		rules := []models.AffinityRule{
			{
				Name: "user-affinity",
				Match: models.AffinityMatchRule{
					PathRegex: "/v1/.*",
				},
				KeySource: models.AffinityKeySource{
					Type: "body_json",
					Path: "user",
				},
			},
		}

		result := am.ExtractValue(c, body, "gpt-4", rules)
		if result.Value != "user-123" {
			t.Errorf("Expected 'user-123', got '%s'", result.Value)
		}
	})

	t.Run("body_regex extraction", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

		body := []byte(`{"prompt_cache_key": "cache-xyz-789"}`)
		rules := []models.AffinityRule{
			{
				Name: "cache-key-affinity",
				Match: models.AffinityMatchRule{
					PathRegex: "/v1/.*",
				},
				KeySource: models.AffinityKeySource{
					Type:    "body_regex",
					Pattern: `"prompt_cache_key"\s*:\s*"(?P<value>[^"]+)"`,
				},
			},
		}

		result := am.ExtractValue(c, body, "gpt-4", rules)
		if result.Value != "cache-xyz-789" {
			t.Errorf("Expected 'cache-xyz-789', got '%s'", result.Value)
		}
	})

	t.Run("path regex mismatch", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/other/path", nil)
		c.Request.Header.Set("X-Session-Id", "session-abc")

		rules := []models.AffinityRule{
			{
				Name: "session-affinity",
				Match: models.AffinityMatchRule{
					PathRegex: "/v1/chat/.*",
				},
				KeySource: models.AffinityKeySource{
					Type: "header",
					Key:  "X-Session-Id",
				},
			},
		}

		result := am.ExtractValue(c, nil, "gpt-4", rules)
		if result.Value != "" {
			t.Errorf("Expected empty value for path mismatch, got '%s'", result.Value)
		}
	})

	t.Run("model regex mismatch", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		c.Request.Header.Set("X-Session-Id", "session-abc")

		rules := []models.AffinityRule{
			{
				Name: "session-affinity",
				Match: models.AffinityMatchRule{
					ModelRegex: "^claude-.*$",
				},
				KeySource: models.AffinityKeySource{
					Type: "header",
					Key:  "X-Session-Id",
				},
			},
		}

		result := am.ExtractValue(c, nil, "gpt-4", rules)
		if result.Value != "" {
			t.Errorf("Expected empty value for model mismatch, got '%s'", result.Value)
		}
	})

	t.Run("multiple rules first match wins", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
		c.Request.Header.Set("X-Session-Id", "session-123")
		c.Request.Header.Set("X-Tenant-Id", "tenant-456")

		rules := []models.AffinityRule{
			{
				Name: "session-rule",
				Match: models.AffinityMatchRule{
					PathRegex: ".*",
				},
				KeySource: models.AffinityKeySource{
					Type: "header",
					Key:  "X-Session-Id",
				},
			},
			{
				Name: "tenant-rule",
				Match: models.AffinityMatchRule{
					PathRegex: ".*",
				},
				KeySource: models.AffinityKeySource{
					Type: "header",
					Key:  "X-Tenant-Id",
				},
			},
		}

		result := am.ExtractValue(c, nil, "gpt-4", rules)
		if result.Value != "session-123" {
			t.Errorf("Expected first rule to match, got '%s'", result.Value)
		}
		if result.MatchedRule.Name != "session-rule" {
			t.Errorf("Expected matched rule 'session-rule', got '%s'", result.MatchedRule.Name)
		}
	})

	t.Run("empty rules", func(t *testing.T) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)

		result := am.ExtractValue(c, nil, "gpt-4", nil)
		if result.Value != "" {
			t.Errorf("Expected empty value for empty rules, got '%s'", result.Value)
		}
	})
}

func TestMappingOperations(t *testing.T) {
	memStore := store.NewMemoryStore()
	am := NewAffinityManager(memStore)

	t.Run("set and get mapping", func(t *testing.T) {
		err := am.SetMapping(1, "abc123", 42, 10*time.Second)
		if err != nil {
			t.Fatalf("SetMapping failed: %v", err)
		}

		val, err := am.GetMapping(1, "abc123")
		if err != nil {
			t.Fatalf("GetMapping failed: %v", err)
		}
		if val != "42" {
			t.Errorf("Expected '42', got '%s'", val)
		}
	})

	t.Run("get non-existent mapping", func(t *testing.T) {
		val, err := am.GetMapping(1, "nonexistent")
		if err != nil {
			t.Fatalf("GetMapping failed: %v", err)
		}
		if val != "" {
			t.Errorf("Expected empty string, got '%s'", val)
		}
	})

	t.Run("different groups isolated", func(t *testing.T) {
		err := am.SetMapping(1, "hash1", 10, 10*time.Second)
		if err != nil {
			t.Fatalf("SetMapping failed: %v", err)
		}
		err = am.SetMapping(2, "hash1", 20, 10*time.Second)
		if err != nil {
			t.Fatalf("SetMapping failed: %v", err)
		}

		val1, _ := am.GetMapping(1, "hash1")
		val2, _ := am.GetMapping(2, "hash1")
		if val1 != "10" || val2 != "20" {
			t.Errorf("Expected group isolation: group1=%s, group2=%s", val1, val2)
		}
	})

	t.Run("overwrite mapping", func(t *testing.T) {
		am.SetMapping(1, "overwrite-test", 100, 10*time.Second)
		am.SetMapping(1, "overwrite-test", 200, 10*time.Second)

		val, _ := am.GetMapping(1, "overwrite-test")
		if val != "200" {
			t.Errorf("Expected '200', got '%s'", val)
		}
	})
}

func TestGetEffectiveTTL(t *testing.T) {
	t.Run("rule TTL", func(t *testing.T) {
		rule := &models.AffinityRule{TTL: 600}
		ttl := GetEffectiveTTL(rule, 3600)
		if ttl != 600*time.Second {
			t.Errorf("Expected 600s, got %v", ttl)
		}
	})

	t.Run("default TTL", func(t *testing.T) {
		rule := &models.AffinityRule{}
		ttl := GetEffectiveTTL(rule, 3600)
		if ttl != 3600*time.Second {
			t.Errorf("Expected 3600s, got %v", ttl)
		}
	})

	t.Run("fallback TTL", func(t *testing.T) {
		ttl := GetEffectiveTTL(nil, 0)
		if ttl != 3600*time.Second {
			t.Errorf("Expected 3600s fallback, got %v", ttl)
		}
	})
}

func TestExtractModelFromBody(t *testing.T) {
	tests := []struct {
		name     string
		body     string
		expected string
	}{
		{
			name:     "standard model field",
			body:     `{"model": "gpt-4", "messages": []}`,
			expected: "gpt-4",
		},
		{
			name:     "no model field",
			body:     `{"messages": []}`,
			expected: "",
		},
		{
			name:     "empty body",
			body:     "",
			expected: "",
		},
		{
			name:     "nested model",
			body:     `{"data": {"model": "gpt-4"}}`,
			expected: "", // model is not at top level
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ExtractModelFromBody([]byte(tt.body))
			if result != tt.expected {
				t.Errorf("ExtractModelFromBody(%q) = %q, want %q", tt.body, result, tt.expected)
			}
		})
	}
}
