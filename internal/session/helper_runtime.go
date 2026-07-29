package session

import (
	"context"
	"fmt"

	"github.com/fengqi-dev/kube-loop/internal/helper"
	"github.com/fengqi-dev/kube-loop/internal/singbox"
)

func newSingboxRuntime() *singbox.Runtime {
	runtime := &singbox.Runtime{}
	if helper.Disabled() {
		return runtime
	}
	runtime.PrivilegedStart = func(
		ctx context.Context, binaryPath, workDir string,
	) (func(context.Context) error, error) {
		if err := helper.EnsureInstall(ctx); err != nil {
			return nil, fmt.Errorf("ensure privileged helper: %w", err)
		}
		client, err := helper.NewClient()
		if err != nil {
			return nil, err
		}
		if _, err := client.Start(ctx, workDir, binaryPath); err != nil {
			return nil, fmt.Errorf("helper start session: %w", err)
		}
		return func(stopCtx context.Context) error {
			_, err := client.Stop(stopCtx, workDir)
			return err
		}, nil
	}
	return runtime
}
