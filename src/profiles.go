package main

// profiles.go —— kdbx 源的临时 settings 文件管理 + profile-not-found 哨兵错误。
// local profile CRUD 已删除 (Phase 4+ 简化:ccc 只走 kdbx)。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// ErrProfileNotFound —— kdbx 也找不到该 profile 名时返回,
// cli 据此分支打印 "可用列表" 提示。
var ErrProfileNotFound = errors.New("profile not found")

// resolveProfile —— kdbx 唯一源。
// 合成 settings JSON 字节 → 写临时文件 → 返回 (Path, TempPath 用于 defer 清理)。
//
// 不存在 / 不可达 / 找不到该 profile 名 → 报 ErrProfileNotFound + 附可用列表。
func resolveProfile(name string) (profileRef, error) {
	if name == "" {
		return profileRef{}, fmt.Errorf("ccc: profile name is empty")
	}
	data, err := LoadProfile(name)
	if err != nil {
		if !errors.Is(err, ErrProfileNotFound) {
			// kdbx 不可达 / 解码错:仍附可用列表
			avail := listKdbxProfileNames()
			hint := joinNames(avail)
			if hint != "" {
				return profileRef{}, fmt.Errorf("ccc: %w: %s\nAvailable profiles: %s",
					ErrProfileNotFound, name, hint)
			}
			return profileRef{}, fmt.Errorf("ccc: %w: %s\n(kdbx unreachable; check %s)",
				ErrProfileNotFound, name, kdbxPath())
		}
		// 真没找到
		avail := listKdbxProfileNames()
		hint := joinNames(avail)
		if hint != "" {
			return profileRef{}, fmt.Errorf("ccc: %w: %s\nAvailable profiles: %s",
				ErrProfileNotFound, name, hint)
		}
		return profileRef{}, fmt.Errorf("ccc: %w: %s\n(no profiles; check kdbx group %s at %s)",
			ErrProfileNotFound, name, kdbxGroup(), kdbxPath())
	}
	tmp, terr := WriteTempSettings(data)
	if terr != nil {
		return profileRef{}, fmt.Errorf("write temp settings: %w", terr)
	}
	return profileRef{Path: tmp, TempPath: tmp}, nil
}

// listKdbxProfileNames —— kdbx 不可达时降级返回空切片(不报错)。
// 供 resolveProfile 的 "Available profiles" 提示用。
func listKdbxProfileNames() []string {
	ks, err := ListProfiles()
	if err != nil {
		return nil
	}
	return ks
}

func joinNames(names []string) string {
	if len(names) == 0 {
		return ""
	}
	return strings.Join(names, ", ")
}

// ---------- 临时 settings 文件 (kdbx 源) ----------

// WriteTempSettings 写 settings 字节到系统临时目录;权限 0600;
// 返回绝对路径(claude 用 --settings 引用)。
//
// 文件名格式: ccc-settings-<随机>.json —— CleanupTempFiles 据此清残留。
// 调用方负责调用后 os.Remove(若不希望保留)。
func WriteTempSettings(data []byte) (string, error) {
	f, err := os.CreateTemp("", "ccc-settings-*.json")
	if err != nil {
		return "", fmt.Errorf("create temp: %w", err)
	}
	path := f.Name()
	if err := os.Chmod(path, 0o600); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("chmod temp: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("write temp: %w", err)
	}
	if err := f.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("close temp: %w", err)
	}
	return path, nil
}

// CleanupTempFiles 启动时清掉所有 ccc-settings-*.json 残留。
// best-effort,失败不报错(留给下次启动再清)。
func CleanupTempFiles() {
	dir := os.TempDir()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, e := range entries {
		name := e.Name()
		if !strings.HasPrefix(name, "ccc-settings-") || !strings.HasSuffix(name, ".json") {
			continue
		}
		_ = os.Remove(filepath.Join(dir, name))
	}
}