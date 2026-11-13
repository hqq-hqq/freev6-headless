//go:build !windows

package network

import (
	"context"
	"errors"
)

var ErrWindowsOnly = errors.New("Windows network snapshot is only available on Windows")

// 非 Windows 平台不做快照/还原：mihomo TUN 的 auto-route 路由由内核进程
// 生命周期自管（退出即回收），DNS 也不落盘改动，无需 Windows 版的
// netsh 级网络基线。返回空 Snapshot 让 CLI/helper 的 start/stop 原样跑通。
func CaptureSnapshot(context.Context, bool) (Snapshot, error) {
	return Snapshot{}, nil
}
