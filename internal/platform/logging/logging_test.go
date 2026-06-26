package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"

	"github.com/huseyn0w/cmstack-go/internal/platform/config"
)

func TestLevelFor(t *testing.T) {
	cases := map[string]slog.Level{
		"production":  slog.LevelInfo,
		"test":        slog.LevelWarn,
		"development": slog.LevelDebug,
		"anything":    slog.LevelDebug,
	}
	for env, want := range cases {
		if got := levelFor(env); got != want {
			t.Errorf("levelFor(%q) = %v, want %v", env, got, want)
		}
	}
}

func TestNewWithWriterEmitsJSON(t *testing.T) {
	var buf bytes.Buffer
	logger := NewWithWriter(config.Config{AppEnv: "development"}, &buf)
	logger.Info("hello", "key", "value")

	line := strings.TrimSpace(buf.String())
	var m map[string]any
	if err := json.Unmarshal([]byte(line), &m); err != nil {
		t.Fatalf("log line is not JSON: %v (%q)", err, line)
	}
	if m["msg"] != "hello" || m["key"] != "value" {
		t.Errorf("unexpected log fields: %v", m)
	}
}
