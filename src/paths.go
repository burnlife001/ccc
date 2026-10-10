package main

// paths.go —— 版本号唯一来源 + exe 自身路径与 .env 加载。
//
// local profile 源已删除,不再有 baseDir / cccDir / profilesDir 之类的
// 路径常量;kdbx 源的路径集中在 src/kdbx.go (kdbxPath / credPathFor)。
// 凡是机器相关的路径(如 CCC_KDBX_PATH)一律不写死在源码里,
// 由 <exeDir>/.env 或环境变量提供,本文件的 loadDotEnv 负责装载。
//
// 编译时通过 -ldflags "-X main.version=..." 注入语义化版本号,
// 覆盖默认值 0.1.0-dev。build.ps1 在每次构建时自动注入。

import (
	"os"
	"path/filepath"
	"strings"
)

// version 唯一来源。
var version = "0.1.0-dev"

// exeDir 返回 ccc.exe 所在目录;.env 与 .ccc/ 都锚定在这里。
func exeDir() string {
	exe, err := os.Executable()
	if err != nil {
		return "."
	}
	return filepath.Dir(exe)
}

// loadDotEnv 启动时把 <exeDir>/.env 的 KEY=VALUE 装进进程环境。
// 已有环境变量优先(.env 只补缺);文件不存在/不可读静默跳过。
// 支持 # 注释、空行、值两侧可选的单/双引号。
func loadDotEnv() {
	data, err := os.ReadFile(filepath.Join(exeDir(), ".env"))
	if err != nil {
		return
	}
	for _, kv := range parseDotEnv(data) {
		if os.Getenv(kv[0]) == "" {
			os.Setenv(kv[0], kv[1]) // best-effort: 个别 key 失败不阻断
		}
	}
}

// parseDotEnv 纯函数,便于单测。返回有序的 (key, value) 对。
func parseDotEnv(data []byte) [][2]string {
	var out [][2]string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k = strings.TrimSpace(k)
		v = strings.Trim(strings.TrimSpace(v), `"'`)
		if k != "" {
			out = append(out, [2]string{k, v})
		}
	}
	return out
}