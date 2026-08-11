package codex

import (
	"os"
	"strconv"
	"strings"

	"github.com/haowang02/agent-session-cleaner/internal/agent"
)

const (
	binaryEnv              = "ASC_CODEX_BIN"
	bulkConcurrencyEnv     = "ASC_CODEX_CONCURRENCY"
	defaultBulkConcurrency = 4
)

func configuredBinary() string {
	if override := strings.TrimSpace(os.Getenv(binaryEnv)); override != "" {
		return agent.ExpandHome(override)
	}
	if native := platformNativeBinary(); native != "" {
		return native
	}
	return Binary
}

func configuredBulkConcurrency() int {
	raw := strings.TrimSpace(os.Getenv(bulkConcurrencyEnv))
	limit, err := strconv.Atoi(raw)
	if err == nil && limit > 0 {
		return limit
	}
	return defaultBulkConcurrency
}
