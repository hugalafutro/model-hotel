package debuglog

import (
	"log/slog"
	"testing"
)

func TestInit(t *testing.T) {
	t.Run("debug true sets LevelDebug", func(t *testing.T) {
		Init(true)
		if got := Level(); got != slog.LevelDebug {
			t.Errorf("Level() = %v, want %v", got, slog.LevelDebug)
		}
	})

	t.Run("debug false with no env sets LevelInfo", func(t *testing.T) {
		t.Setenv("DEBUG_LOG", "")
		Init(false)
		if got := Level(); got != slog.LevelInfo {
			t.Errorf("Level() = %v, want %v", got, slog.LevelInfo)
		}
	})

	t.Run("debug false with DEBUG_LOG=true still sets LevelDebug", func(t *testing.T) {
		t.Setenv("DEBUG_LOG", "true")
		Init(false)
		if got := Level(); got != slog.LevelDebug {
			t.Errorf("Level() = %v, want %v", got, slog.LevelDebug)
		}
	})
}

func TestIsDebugLogEnv(t *testing.T) {
	tests := []struct {
		name string
		env  string
		want bool
	}{
		{"true", "true", true},
		{"1", "1", true},
		{"yes", "yes", true},
		{"TRUE", "TRUE", true},
		{"Yes", "Yes", true},
		{"empty", "", false},
		{"false", "false", false},
		{"0", "0", false},
		{"no", "no", false},
		{"random", "maybe", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("DEBUG_LOG", tt.env)
			if got := isDebugLogEnv(); got != tt.want {
				t.Errorf("isDebugLogEnv() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestLevel(t *testing.T) {
	t.Run("after Init(true) returns Debug", func(t *testing.T) {
		Init(true)
		if got := Level(); got != slog.LevelDebug {
			t.Errorf("Level() = %v, want %v", got, slog.LevelDebug)
		}
	})

	t.Run("after Init(false) returns Info", func(t *testing.T) {
		t.Setenv("DEBUG_LOG", "")
		Init(false)
		if got := Level(); got != slog.LevelInfo {
			t.Errorf("Level() = %v, want %v", got, slog.LevelInfo)
		}
	})
}
