package main

// profiles.go —— profile 文件的纯文件操作。
// 不做字段校验(零转换透传),只校验"文件存在 + 是合法 JSON"。

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
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
func resolveProfile(name string) (string, error) {
	if err := validateProfileName(name); err != nil {
		return "", err
	}
	p := profilePath(name)
	info, err := os.Stat(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			avail, _ := listProfileNames()
			if len(avail) > 0 {
				return "", fmt.Errorf("ccc: %w: %s\nAvailable profiles: %s",
					ErrProfileNotFound, name, strings.Join(avail, ", "))
			}
			return "", fmt.Errorf("ccc: %w: %s\n(no profiles installed; run `ccc add <name>` first)",
				ErrProfileNotFound, name)
		}
		return "", fmt.Errorf("stat profile %s: %w", name, err)
	}
	if info.IsDir() {
		return "", fmt.Errorf("ccc: profile path is a directory: %s", p)
	}
	if err := validateJSONFile(p); err != nil {
		return "", fmt.Errorf("ccc: invalid JSON in %s: %w", p, err)
	}
	return p, nil
}

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

// removeProfile 删除 profile;不存在视为错误(用户输入)。
func removeProfile(name string) error {
	if err := validateProfileName(name); err != nil {
		return err
	}
	p := profilePath(name)
	err := os.Remove(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("ccc: %w: %s", ErrProfileNotFound, name)
		}
		return err
	}
	return nil
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
