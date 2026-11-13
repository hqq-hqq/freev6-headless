//go:build !windows

package network

import "context"

func RestoreSnapshot(context.Context, Snapshot) error {
	return nil
}

func RestoreHotspot(context.Context, Snapshot) error {
	return nil
}

func RestoreDNS(context.Context, Snapshot) {}
