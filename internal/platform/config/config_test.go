package config

import (
	"os"
	"testing"
	"time"
)

func TestLoad(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantErr bool
		check   func(t *testing.T, c Config)
	}{
		{
			name: "minimal required vars apply defaults",
			env: map[string]string{
				"DATABASE_URL": "postgres://localhost/cms",
				"SESSION_KEY":  "secret",
			},
			check: func(t *testing.T, c Config) {
				if c.AppEnv != "development" {
					t.Errorf("AppEnv = %q, want development", c.AppEnv)
				}
				if c.HTTPAddr != ":8080" {
					t.Errorf("HTTPAddr = %q, want :8080", c.HTTPAddr)
				}
				if !c.IsDevelopment() || c.IsProduction() {
					t.Errorf("env predicates wrong for %q", c.AppEnv)
				}
			},
		},
		{
			name: "production overrides",
			env: map[string]string{
				"DATABASE_URL": "postgres://localhost/cms",
				"SESSION_KEY":  "secret",
				"APP_ENV":      "production",
				"HTTP_ADDR":    ":9090",
			},
			check: func(t *testing.T, c Config) {
				if !c.IsProduction() {
					t.Errorf("expected production env")
				}
				if c.HTTPAddr != ":9090" {
					t.Errorf("HTTPAddr = %q, want :9090", c.HTTPAddr)
				}
			},
		},
		{
			name:    "missing DATABASE_URL fails",
			env:     map[string]string{"SESSION_KEY": "secret"},
			wantErr: true,
		},
		{
			name: "missing SESSION_KEY is allowed (reserved, not required)",
			env:  map[string]string{"DATABASE_URL": "postgres://localhost/cms"},
			check: func(t *testing.T, c Config) {
				if c.SessionKey != "" {
					t.Errorf("SessionKey = %q, want empty default", c.SessionKey)
				}
			},
		},
		{
			name: "duration defaults parse",
			env: map[string]string{
				"DATABASE_URL": "postgres://localhost/cms",
			},
			check: func(t *testing.T, c Config) {
				if c.ReadTimeout != 15*time.Second {
					t.Errorf("ReadTimeout = %v, want 15s", c.ReadTimeout)
				}
				if c.WriteTimeout != 30*time.Second {
					t.Errorf("WriteTimeout = %v, want 30s", c.WriteTimeout)
				}
				if c.ShutdownTimeout != 15*time.Second {
					t.Errorf("ShutdownTimeout = %v, want 15s", c.ShutdownTimeout)
				}
			},
		},
		{
			name: "malformed duration fails fast",
			env: map[string]string{
				"DATABASE_URL":      "postgres://localhost/cms",
				"HTTP_READ_TIMEOUT": "15x",
			},
			wantErr: true,
		},
		{
			name: "storage driver defaults to local",
			env: map[string]string{
				"DATABASE_URL": "postgres://localhost/cms",
			},
			check: func(t *testing.T, c Config) {
				if c.StorageDriver != "local" {
					t.Errorf("StorageDriver = %q, want local", c.StorageDriver)
				}
				if c.MediaMaxBytes != 10485760 {
					t.Errorf("MediaMaxBytes = %d, want 10485760 (10 MiB)", c.MediaMaxBytes)
				}
			},
		},
		{
			name: "s3 storage vars apply",
			env: map[string]string{
				"DATABASE_URL":      "postgres://localhost/cms",
				"STORAGE_DRIVER":    "s3",
				"S3_BUCKET":         "my-media",
				"S3_REGION":         "eu-central-1",
				"S3_ENDPOINT":       "https://r2.example.com",
				"S3_USE_PATH_STYLE": "true",
				"MEDIA_MAX_BYTES":   "5242880",
			},
			check: func(t *testing.T, c Config) {
				if c.StorageDriver != "s3" || c.S3Bucket != "my-media" || c.S3Region != "eu-central-1" {
					t.Errorf("s3 config wrong: %+v", c)
				}
				if !c.S3UsePathStyle {
					t.Error("S3UsePathStyle should be true")
				}
				if c.MediaMaxBytes != 5242880 {
					t.Errorf("MediaMaxBytes = %d, want 5242880", c.MediaMaxBytes)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Clear potentially inherited vars, then set the case's vars.
			// t.Setenv guarantees restoration; Unsetenv ensures "required" tags
			// observe a genuinely absent variable.
			for _, k := range []string{"DATABASE_URL", "SESSION_KEY", "APP_ENV", "HTTP_ADDR", "REDIS_URL", "BASE_URL", "HTTP_READ_TIMEOUT", "HTTP_WRITE_TIMEOUT", "SHUTDOWN_TIMEOUT", "STORAGE_DRIVER", "MEDIA_MAX_BYTES", "S3_BUCKET", "S3_REGION", "S3_ENDPOINT", "S3_USE_PATH_STYLE"} {
				t.Setenv(k, "")
				if err := os.Unsetenv(k); err != nil {
					t.Fatalf("unset %s: %v", k, err)
				}
			}
			for k, v := range tt.env {
				t.Setenv(k, v)
			}
			cfg, err := Load()
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got nil")
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if tt.check != nil {
				tt.check(t, cfg)
			}
		})
	}
}
