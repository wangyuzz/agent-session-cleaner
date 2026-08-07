//go:build !windows

package execx

import (
	"context"
	"os/exec"
)

func newProcess(ctx context.Context, name string, args []string) (*exec.Cmd, error) {
	path, err := exec.LookPath(name)
	if err != nil {
		return nil, err
	}
	return exec.CommandContext(ctx, path, args...), nil
}
