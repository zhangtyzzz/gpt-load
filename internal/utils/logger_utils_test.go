package utils

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/sirupsen/logrus"
)

func TestOpenSecureLogFileCreatesAndTightensPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "app.log")
	if err := os.WriteFile(path, []byte("legacy\n"), 0644); err != nil {
		t.Fatalf("create legacy log: %v", err)
	}
	if err := os.Chmod(path, 0644); err != nil {
		t.Fatalf("set legacy permissions: %v", err)
	}

	file, err := openSecureLogFile(path)
	if err != nil {
		t.Fatalf("openSecureLogFile: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close log: %v", err)
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat log: %v", err)
	}
	if got := info.Mode().Perm(); got != 0600 {
		t.Fatalf("log permissions = %o, want 600", got)
	}
}

func TestCredentialRedactionHookSanitizesMessagesAndFields(t *testing.T) {
	const fieldSecret = "field-secret"
	const errorSecret = "error-secret"
	entry := &logrus.Entry{
		Message: "request to https://example.test?access_token=message-secret failed",
		Data: logrus.Fields{
			"auth_key": fieldSecret,
			"error":    errors.New("GET https://example.test?key=" + errorSecret),
			"model":    "gpt-4",
		},
	}
	if err := (credentialRedactionHook{}).Fire(entry); err != nil {
		t.Fatalf("redaction hook: %v", err)
	}
	rendered := entry.Message + " " + fmt.Sprint(entry.Data)
	for _, secret := range []string{"message-secret", fieldSecret, errorSecret} {
		if strings.Contains(rendered, secret) {
			t.Fatalf("redaction hook leaked %q: %s", secret, rendered)
		}
	}
	if entry.Data["model"] != "gpt-4" {
		t.Fatalf("redaction hook changed safe field: %v", entry.Data["model"])
	}
}
