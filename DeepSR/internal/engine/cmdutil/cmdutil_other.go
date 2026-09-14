//go:build !windows

package cmdutil

import (
	"context"
	"os/exec"
)

func Command(name string, arg ...string) *exec.Cmd {
	return exec.Command(name, arg...)
}

func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	return exec.CommandContext(ctx, name, arg...)
}

func HideWindow(cmd *exec.Cmd) {}

func RefreshEnvironmentPath() []string {
	return nil
}

func KillProcessTree(pid int) error {
	return nil
}

func InitProcessJob() {}

func RegisterPid(pid int) {}

func UnregisterPid(pid int) {}

func KillAllEngineProcesses() {}

func HasActivePids() bool {
	return false
}
