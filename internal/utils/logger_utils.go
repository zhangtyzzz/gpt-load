package utils

import (
	"gpt-load/internal/types"
	"io"
	"os"
	"path/filepath"

	"github.com/sirupsen/logrus"
)

// SetupLogger configures the logging system based on the provided configuration.
func SetupLogger(configManager types.ConfigManager) {
	logConfig := configManager.GetLogConfig()
	logrus.AddHook(credentialRedactionHook{})

	// Set log level
	level, err := logrus.ParseLevel(logConfig.Level)
	if err != nil {
		logrus.Warn("Invalid log level, using info")
		level = logrus.InfoLevel
	}
	logrus.SetLevel(level)

	// Set log format
	if logConfig.Format == "json" {
		logrus.SetFormatter(&logrus.JSONFormatter{
			TimestampFormat: "2006-01-02T15:04:05.000Z07:00", // ISO 8601 format
		})
	} else {
		logrus.SetFormatter(&logrus.TextFormatter{
			FullTimestamp:   true,
			TimestampFormat: "2006-01-02 15:04:05",
		})
	}

	// Setup file logging if enabled
	if logConfig.EnableFile {
		logDir := filepath.Dir(logConfig.FilePath)
		if err := os.MkdirAll(logDir, 0750); err != nil {
			logrus.Warnf("Failed to create log directory: %v", err)
		} else {
			logFile, err := openSecureLogFile(logConfig.FilePath)
			if err != nil {
				logrus.Warnf("Failed to open log file: %v", err)
			} else {
				logrus.SetOutput(io.MultiWriter(os.Stdout, logFile))
			}
		}
	}
}

type credentialRedactionHook struct{}

func (credentialRedactionHook) Levels() []logrus.Level {
	return logrus.AllLevels
}

func (credentialRedactionHook) Fire(entry *logrus.Entry) error {
	entry.Message = SanitizeText(entry.Message)
	for name, value := range entry.Data {
		if IsSensitiveName(name) {
			entry.Data[name] = RedactedValue
			continue
		}
		switch typedValue := value.(type) {
		case string:
			entry.Data[name] = SanitizeText(typedValue)
		case error:
			entry.Data[name] = SanitizeText(typedValue.Error())
		}
	}
	return nil
}

func openSecureLogFile(path string) (*os.File, error) {
	// #nosec G304 -- path is the operator-configured log destination; arbitrary placement is intentional and the file is forced to mode 0600 below.
	logFile, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		return nil, err
	}

	// OpenFile's permission is only applied to newly-created files. Tighten an
	// existing file created by an earlier, more permissive version as well.
	if err := logFile.Chmod(0600); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	return logFile, nil
}
