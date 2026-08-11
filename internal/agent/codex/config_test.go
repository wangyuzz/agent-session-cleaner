package codex

import "testing"

func TestConfiguredBulkConcurrency(t *testing.T) {
	t.Setenv(bulkConcurrencyEnv, "8")
	if got := configuredBulkConcurrency(); got != 8 {
		t.Errorf("configuredBulkConcurrency() = %d, want 8", got)
	}

	t.Setenv(bulkConcurrencyEnv, "not-a-number")
	if got := configuredBulkConcurrency(); got != defaultBulkConcurrency {
		t.Errorf("invalid setting gave %d, want default %d", got, defaultBulkConcurrency)
	}
}

func TestConfiguredBinaryHonoursTheEnvironment(t *testing.T) {
	t.Setenv(binaryEnv, "  /custom/codex  ")
	if got := configuredBinary(); got != "/custom/codex" {
		t.Errorf("configuredBinary() = %q, want the override", got)
	}
}
