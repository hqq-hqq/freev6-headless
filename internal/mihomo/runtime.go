package mihomo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
)

var ErrBinaryNotFound = errors.New("mihomo runtime binary not found")

// runtimeBinNames 按优先级返回当前平台的内核文件名：微架构变体（v3/v2）
// 优先，其次是无变体名，最后是通用名。Windows 带 .exe 后缀；Linux 无后缀，
// 且额外接受 -v2（download.go 的 FREEV6_MIHOMO_AMD64_VARIANT=v2 降级产物）。
func runtimeBinNames() []string {
	base := "mihomo-" + runtime.GOOS + "-" + runtime.GOARCH
	if runtime.GOOS == "windows" {
		return []string{base + "-v3.exe", base + ".exe", "mihomo.exe"}
	}
	return []string{base + "-v3", base + "-v2", base, "mihomo"}
}

// RuntimeCandidates returns paths in preference order for the current platform.
// The runtime directory is the installed layout; state is retained for local development.
func RuntimeCandidates(root string) []string {
	var candidates []string
	for _, dir := range []string{"runtime", "state"} {
		for _, name := range runtimeBinNames() {
			candidates = append(candidates, filepath.Join(root, dir, name))
		}
	}
	return candidates
}

func DiscoverBinary(root string) (string, error) {
	if root == "" {
		root = "."
	}
	for _, candidate := range RuntimeCandidates(root) {
		info, err := os.Stat(candidate)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return "", fmt.Errorf("inspect mihomo runtime %q: %w", candidate, err)
		}
		if info.IsDir() {
			continue
		}
		return candidate, nil
	}
	return "", fmt.Errorf("%w under %q", ErrBinaryNotFound, root)
}

func ResolveBinary(explicit, root string) (string, error) {
	if explicit != "" {
		info, err := os.Stat(explicit)
		if err != nil {
			return "", fmt.Errorf("inspect mihomo binary %q: %w", explicit, err)
		}
		if info.IsDir() {
			return "", fmt.Errorf("mihomo binary path %q is a directory", explicit)
		}
		return explicit, nil
	}
	return DiscoverBinary(root)
}
