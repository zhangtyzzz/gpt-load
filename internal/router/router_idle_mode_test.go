package router

import "testing"

func TestDatabaseMaintenanceOnlyFollowsRealApplicationTraffic(t *testing.T) {
	tests := []struct {
		path string
		want bool
	}{
		{path: "/health", want: false},
		{path: "/", want: false},
		{path: "/assets/app.js", want: false},
		{path: "/api/status", want: true},
		{path: "/api/auth/login", want: true},
		{path: "/proxy/openai/v1/chat/completions", want: true},
	}

	for _, test := range tests {
		if got := shouldNotifyDatabaseMaintenance(test.path); got != test.want {
			t.Errorf("shouldNotifyDatabaseMaintenance(%q) = %t, want %t", test.path, got, test.want)
		}
	}
}
