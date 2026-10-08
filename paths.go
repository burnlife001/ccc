package main

// paths.go —— 唯一允许计算路径的文件。
// 业务代码禁止出现 filepath.Join(home, ...) 字面量。

import (
	"os"
	"path/filepath"
)

// version 唯一来源;编译时通过 -ldflags "-X main.version=..." 注入。
// 默认值仅在 `go run` 场景下生效,build.ps1 会覆盖。
var version = "0.1.0-dev"

func homeDir() string {
	if h := os.Getenv("CCC_HOME"); h != "" {
		return h
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		// fallback:尽量不要发生;若发生,用当前目录,不 panic。
		return "."
	}
	return home
}

func cccDir() string {
	return filepath.Join(homeDir(), ".ccc")
}

func profilesDir() string {
	return filepath.Join(cccDir(), "profiles")
}

func sessionsFile() string {
	return filepath.Join(cccDir(), "sessions.json")
}

func installDir() string {
	// 默认安装目录;build.ps1 用此路径。
	return `D:\Programs\ccc`
}

// profilePath 返回 ~/.ccc/profiles/<name>.json。
// cli / profiles 业务代码都从这里取,不在别处拼路径。
func profilePath(name string) string {
	return filepath.Join(profilesDir(), name+".json")
}

// ensureCccDirs 一次性创建 ~/.ccc 和 ~/.ccc/profiles。
// 已存在不报错。
func ensureCccDirs() error {
	for _, d := range []string{cccDir(), profilesDir()} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	return nil
}
