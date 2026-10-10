package main

// profiles.go —— profile 文件的纯文件操作 + 临时 settings 文件管理。
// 不做字段校验(零转换透传),只校验"文件存在 + 是合法 JSON"。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// ErrProfileNotFound —— main 据此分支打印 "可用列表" 提示。
var ErrProfileNotFound = errors.New("profile not found")

// validateProfileName 阻止路径穿越字符;非空。
func validateProfileName(name string) error {
	if name == "" {
		return fmt.Errorf("ccc: profile name is empty")
	}
	// 拒绝路径分隔符、点(隐藏/相对)、NUL
	if strings.ContainsAny(name, `/\.`) || strings.ContainsRune(name, 0) {
		return fmt.Errorf("ccc: invalid profile name: %q", name)
	}
	return nil
}

// resolveProfile 校验 profile 存在且 JSON 合法,返回绝对路径。
// 不存在时附带可用 profile 列表;JSON 非法时附带文件名。
//
// 双源查找: 先本地 profiles/<name>.json;找不到再 kdbx。
// kdbx 源会写真临时文件 + 标记 TempPath;调用方负责 defer cleanup。
func resolveProfile(name string) (profileRef, error) {
	if err := validateProfileName(name); err != nil {
		return profileRef{}, err
	}
	p := profilePath(name)
	info, err := os.Stat(p)
	if err == nil {
		if info.IsDir() {
			return profileRef{}, fmt.Errorf("ccc: profile path is a directory: %s", p)
		}
		if jerr := validateJSONFile(p); jerr != nil {
			return profileRef{}, fmt.Errorf("ccc: invalid JSON in %s: %w", p, jerr)
		}
		return profileRef{Path: p, Source: "local"}, nil
	}
	if !errors.Is(err, os.ErrNotExist) {
		return profileRef{}, fmt.Errorf("stat profile %s: %w", name, err)
	}

	// 本地不存在 → 尝试 kdbx
	data, kerr := LoadProfile(name)
	if kerr != nil {
		// kdbx 也没找到 → 报 profile not found,附可用列表(双源合并)
		avail := listAllProfileNames()
		if len(avail) > 0 {
			return profileRef{}, fmt.Errorf("ccc: %w: %s\nAvailable profiles: %s",
				ErrProfileNotFound, name, strings.Join(avail, ", "))
		}
		return profileRef{}, fmt.Errorf("ccc: %w: %s\n(no profiles installed; "+
			"drop a settings_*.json into "+profilesDir()+" or set up kdbx group "+kdbxGroup()+")",
			ErrProfileNotFound, name)
	}
	tmp, terr := WriteTempSettings(data)
	if terr != nil {
		return profileRef{}, fmt.Errorf("write temp settings: %w", terr)
	}
	return profileRef{Path: tmp, Source: "kdbx", TempPath: tmp}, nil
}

// (cmdRm 已删除,profileExistsLocal / removeProfile 一起移除;
// kdbx 源删除需求由 kdbx 编辑流程承担。)

// validateJSONFile 解析整个文件,确认是合法 JSON。
// 不限制顶层结构 —— profile 是 settings 文件,字段 schema 我们不关心。
func validateJSONFile(p string) error {
	f, err := os.Open(p)
	if err != nil {
		return err
	}
	defer f.Close()
	dec := json.NewDecoder(f)
	var raw json.RawMessage
	if err := dec.Decode(&raw); err != nil {
		if errors.Is(err, io.EOF) {
			return fmt.Errorf("empty file")
		}
		return err
	}
	// 确保文件没有多余 token(防 "a:1}" 这种截断/拼接)
	if dec.More() {
		return fmt.Errorf("trailing data after JSON value")
	}
	return nil
}

// listProfileNames 列出 profiles 目录下所有 .json,按字母序。
// 目录不存在时返回空切片(非错误)——便于 `ccc list` 在全新环境优雅降级。
func listProfileNames() ([]string, error) {
	entries, err := os.ReadDir(profilesDir())
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return []string{}, nil
		}
		return nil, err
	}
	var names []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		n := e.Name()
		if strings.HasSuffix(n, ".json") {
			names = append(names, strings.TrimSuffix(n, ".json"))
		}
	}
	sort.Strings(names)
	return names, nil
}

// listAllProfileNames —— 双源合并(本地 + kdbx),去重,字母序。
// kdbx 不可达时降级为只列本地,不报错(用于 list 命令 + "Available" 提示)。
func listAllProfileNames() []string {
	seen := map[string]bool{}
	var out []string
	for _, n := range mustListLocal() {
		if !seen[n] {
			seen[n] = true
			out = append(out, n)
		}
	}
	// kdbx 不可达 / 非 Windows / 路径不存在 → 静默跳过
	if ks, err := ListProfiles(); err == nil {
		for _, n := range ks {
			if !seen[n] {
				seen[n] = true
				out = append(out, n)
			}
		}
	}
	sort.Strings(out)
	return out
}

// mustListLocal —— listProfileNames 的吞错版本;供 listAllProfileNames 用。
func mustListLocal() []string {
	ns, err := listProfileNames()
	if err != nil {
		return nil
	}
	return ns
}

// writeProfile 原子写入 profile 文件;权限 0600(token)。
func writeProfile(name string, data []byte) error {
	if err := validateProfileName(name); err != nil {
		return err
	}
	if err := validateJSONBytes(data); err != nil {
		return fmt.Errorf("template: %w", err)
	}
	if err := ensureCccDirs(); err != nil {
		return err
	}
	p := profilePath(name)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// writeProfileFromTemplate 读模板、写入,模板不存在或非法时返回错误。
func writeProfileFromTemplate(name, templatePath string) error {
	data, err := os.ReadFile(templatePath)
	if err != nil {
		return fmt.Errorf("read template: %w", err)
	}
	return writeProfile(name, data)
}

// validateJSONBytes 是 validateJSONFile 的内存版本。
func validateJSONBytes(b []byte) error {
	var raw json.RawMessage
	if err := json.Unmarshal(b, &raw); err != nil {
		return err
	}
	return nil
}

// readProfile 原始字节读出,供透传使用 —— 不解析内容。
func readProfile(p string) ([]byte, error) {
	return os.ReadFile(p)
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
