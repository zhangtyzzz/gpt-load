package channel

type CatalogUpstream struct {
	URL    string `json:"url"`
	Weight int    `json:"weight"`
}

type ChannelCatalogEntry struct {
	ID              string            `json:"id"`
	ChannelType     string            `json:"channel_type"`
	DisplayName     string            `json:"display_name"`
	Description     string            `json:"description"`
	IntegrationKind string            `json:"integration_kind"`
	Upstreams       []CatalogUpstream `json:"upstreams"`
	SuggestedPath   string            `json:"suggested_path"`
	ChannelConfig   GenericHTTPConfig `json:"channel_config"`
}

func GetChannelCatalog() []ChannelCatalogEntry {
	return []ChannelCatalogEntry{
		genericPreset("tavily-http", "Tavily HTTP", "Tavily REST API key rotation", "http_api", "https://api.tavily.com", "", bearerConfig("tavily-http", validation("", "GET", "/usage"), GenericStreamNever, []int{401, 429, 432, 433})),
		genericPreset("exa-http", "Exa HTTP", "Exa REST API key rotation", "http_api", "https://api.exa.ai", "", apiKeyConfig("exa-http", validation("", "GET", "/websets/v0/teams/me"), GenericStreamAuto, []int{401, 402, 429})),
		genericPreset("jina-reader", "Jina Reader", "Jina Reader API key rotation", "http_api", "https://r.jina.ai", "", bearerConfig("jina-reader", validation("https://api.jina.ai", "GET", "/v1/classifiers"), GenericStreamAuto, []int{401, 403, 429})),
		genericPreset("jina-search", "Jina Search", "Jina Search API key rotation", "http_api", "https://s.jina.ai", "", bearerConfig("jina-search", validation("https://api.jina.ai", "GET", "/v1/classifiers"), GenericStreamAuto, []int{401, 403, 429})),
		genericPreset("jina-foundation", "Jina Foundation", "Jina Search Foundation API key rotation", "http_api", "https://api.jina.ai", "", bearerConfig("jina-foundation", validation("", "GET", "/v1/classifiers"), GenericStreamAuto, []int{401, 403, 429})),
		genericPreset("tavily-mcp", "Tavily MCP", "Tavily remote Streamable HTTP MCP", "hosted_mcp", "https://mcp.tavily.com", "/mcp/", bearerConfig("tavily-mcp", validation("https://api.tavily.com", "GET", "/usage"), GenericStreamAuto, []int{401, 429, 432, 433})),
		genericPreset("exa-mcp", "Exa MCP", "Exa remote Streamable HTTP MCP", "hosted_mcp", "https://mcp.exa.ai", "/mcp", apiKeyConfig("exa-mcp", validation("https://api.exa.ai", "GET", "/websets/v0/teams/me"), GenericStreamAuto, []int{401, 402, 429})),
	}
}

func genericPreset(id, name, description, integrationKind, upstream, path string, cfg GenericHTTPConfig) ChannelCatalogEntry {
	return ChannelCatalogEntry{
		ID:              id,
		ChannelType:     GenericHTTPChannelType,
		DisplayName:     name,
		Description:     description,
		IntegrationKind: integrationKind,
		Upstreams:       []CatalogUpstream{{URL: upstream, Weight: 1}},
		SuggestedPath:   path,
		ChannelConfig:   cfg,
	}
}

func bearerConfig(id string, validationConfig GenericHTTPValidationConfig, stream string, retryStatuses []int) GenericHTTPConfig {
	return presetConfig(id, GenericHTTPAuthConfig{
		Placement: GenericAuthHeader,
		Name:      "Authorization",
		Prefix:    "Bearer ",
	}, validationConfig, stream, retryStatuses)
}

func apiKeyConfig(id string, validationConfig GenericHTTPValidationConfig, stream string, retryStatuses []int) GenericHTTPConfig {
	return presetConfig(id, GenericHTTPAuthConfig{
		Placement: GenericAuthHeader,
		Name:      "x-api-key",
		Prefix:    "",
	}, validationConfig, stream, retryStatuses)
}

func presetConfig(id string, auth GenericHTTPAuthConfig, validationConfig GenericHTTPValidationConfig, stream string, retryStatuses []int) GenericHTTPConfig {
	return GenericHTTPConfig{
		Version:    1,
		PresetID:   id,
		Auth:       auth,
		Validation: validationConfig,
		StreamMode: stream,
		Retry: GenericHTTPRetryConfig{
			SafeMethods:      []string{"GET", "HEAD"},
			FailoverStatuses: retryStatuses,
		},
		MaxRequestBodyBytes: defaultGenericRequestBodyLimit,
		MaxErrorBodyBytes:   defaultGenericErrorBodyLimit,
	}
}

func validation(baseURL, method, path string) GenericHTTPValidationConfig {
	return GenericHTTPValidationConfig{
		Enabled:         true,
		BaseURL:         baseURL,
		Method:          method,
		Path:            path,
		Headers:         map[string]string{"Accept": "application/json"},
		Body:            nil,
		ValidStatuses:   []int{200},
		InvalidStatuses: []int{401},
	}
}
