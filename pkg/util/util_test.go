package util

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/component-base/config"
)

func TestGenerateComponentName(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{"NilSlice", nil, defaultSchedulerName},
		{"EmptySlice", []string{}, defaultSchedulerName},
		{"Single", []string{"only"}, "only"},
		{"Multiple", []string{"a", "b"}, defaultSchedulerName},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := GenerateComponentName(tc.input)
			if got != tc.expected {
				t.Errorf("GenerateComponentName(%v) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestGenerateSchedulerName(t *testing.T) {
	tests := []struct {
		name     string
		input    []string
		expected string
	}{
		{"NilSlice", nil, defaultSchedulerName},
		{"EmptySlice", []string{}, defaultSchedulerName},
		{"Single", []string{"sched"}, "sched"},
		{"Multiple", []string{"x", "y"}, "x"},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := GenerateSchedulerName(tc.input)
			if got != tc.expected {
				t.Errorf("GenerateSchedulerName(%v) = %q; want %q", tc.input, got, tc.expected)
			}
		})
	}
}

func TestLeaderElectionDefault(t *testing.T) {
	cfg := &config.LeaderElectionConfiguration{}
	LeaderElectionDefault(cfg)

	t.Run("LeaderElect", func(t *testing.T) {
		if !cfg.LeaderElect {
			t.Error("expected LeaderElect=true")
		}
	})
	t.Run("LeaseDuration", func(t *testing.T) {
		if cfg.LeaseDuration.Duration != 15*time.Second {
			t.Errorf("LeaseDuration = %v; want 15s", cfg.LeaseDuration.Duration)
		}
	})
	t.Run("RenewDeadline", func(t *testing.T) {
		if cfg.RenewDeadline.Duration != 10*time.Second {
			t.Errorf("RenewDeadline = %v; want 10s", cfg.RenewDeadline.Duration)
		}
	})
	t.Run("RetryPeriod", func(t *testing.T) {
		if cfg.RetryPeriod.Duration != 2*time.Second {
			t.Errorf("RetryPeriod = %v; want 2s", cfg.RetryPeriod.Duration)
		}
	})
	t.Run("ResourceLock", func(t *testing.T) {
		if cfg.ResourceLock != resourcelock.LeasesResourceLock {
			t.Errorf("ResourceLock = %q; want %q", cfg.ResourceLock, resourcelock.LeasesResourceLock)
		}
	})
	t.Run("ResourceNamespace", func(t *testing.T) {
		if cfg.ResourceNamespace != defaultLockObjectNamespace {
			t.Errorf("ResourceNamespace = %q; want %q", cfg.ResourceNamespace, defaultLockObjectNamespace)
		}
	})
}

func TestPromHandler(t *testing.T) {
	h := PromHandler()
	if h == nil {
		t.Fatal("PromHandler returned nil")
	}

	// Smoke-test /metrics endpoint
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Errorf("metrics status = %d; want %d", rec.Code, http.StatusOK)
	}
	body := rec.Body.String()
	// Check for Prometheus-style output
	if !strings.Contains(body, "# HELP") || !strings.Contains(body, "# TYPE") {
		t.Error("metrics body missing Prometheus format markers")
	}

	// ensure we can call twice (deregistration path)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK {
		t.Errorf("second metrics call status = %d; want %d", rec2.Code, http.StatusOK)
	}
}

// A simple sanity check that our package-level constants are correct.
func TestDefaultConstants(t *testing.T) {
	if defaultSchedulerName != "volcano" {
		t.Errorf("defaultSchedulerName = %q; want \"volcano\"", defaultSchedulerName)
	}
	if defaultLockObjectNamespace != "volcano-system" {
		t.Errorf("defaultLockObjectNamespace = %q; want \"volcano-system\"", defaultLockObjectNamespace)
	}
	if defaultElectionLeaseDuration.Duration != 15*time.Second {
		t.Errorf("defaultElectionLeaseDuration = %v; want 15s", defaultElectionLeaseDuration)
	}
	if defaultElectionRenewDeadline.Duration != 10*time.Second {
		t.Errorf("defaultElectionRenewDeadline = %v; want 10s", defaultElectionRenewDeadline)
	}
	if defaultElectionRetryPeriod.Duration != 2*time.Second {
		t.Errorf("defaultElectionRetryPeriod = %v; want 2s", defaultElectionRetryPeriod)
	}
}
