package main

import (
	"bytes"
	"strings"
	"testing"
)

func lines(t *testing.T, env string) string {
	t.Helper()
	t.Setenv("SPLASH_LOG_LEVEL", env)
	var buf bytes.Buffer
	log, err := loggerFromEnv(&buf)
	if err != nil {
		t.Fatal(err)
	}
	log.Debug("a debug line")
	log.Info("an info line")
	log.Warn("a warn line")
	log.Error("an error line")
	return buf.String()
}

func TestLogLevelVariableIsHonoured(t *testing.T) {
	for env, want := range map[string][]string{
		"":      {"info", "warn", "error"}, // the default
		"info":  {"info", "warn", "error"},
		"debug": {"debug", "info", "warn", "error"},
		"WARN":  {"warn", "error"},
		"error": {"error"},
	} {
		out := lines(t, env)
		for _, level := range []string{"debug", "info", "warn", "error"} {
			has := strings.Contains(out, "a "+level+" line") || strings.Contains(out, "an "+level+" line")
			expected := false
			for _, w := range want {
				expected = expected || w == level
			}
			if has != expected {
				t.Errorf("SPLASH_LOG_LEVEL=%q: %s line present=%v, want %v\n%s", env, level, has, expected, out)
			}
		}
	}
}

func TestInvalidLogLevelIsAnErrorAtStart(t *testing.T) {
	t.Setenv("SPLASH_LOG_LEVEL", "verbose")
	if _, err := loggerFromEnv(&bytes.Buffer{}); err == nil || !strings.Contains(err.Error(), "SPLASH_LOG_LEVEL") {
		t.Fatalf("err=%v", err)
	}
}
