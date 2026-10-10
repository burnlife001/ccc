// ccc —— kdbx 直读 + 内存合成 settings JSON。
// 唯一允许 import gokeepasslib / syscall.crypt32 的文件;
//
// 阶段路线 (todolist Phase 4):
//   1. readCredDpapi: 读 .kdbx_master.cred 并 CryptUnprotectData (DPAPI, user-scope)
//   2. openAndUnlockKdbx: 解锁 kdbx 并解锁 protected entries
//   3. findGroup: 递归找 cc 组
//   4. composeSettings + serializeSettings: 1:1 复刻 compose_settings.py
//   5. LoadProfile / ListProfiles: 对外 API
//
// 字节级复刻原则 (Phase 4.1 验收):
//   - 顶层字段序: env, permissions, skipDangerousModePermissionPrompt
//   - env 序: extra_env → ANTHROPIC_BASE_URL → AUTH_TOKEN → DEFAULT_*_MODEL → MODEL → SKELETON_ENV
//   - allow 数组单行紧凑 ("Bash","Edit",...)
//   - 2 空格缩进,末尾 \n, 不带 current_Model_Provider
//
// 跨平台: kdbx 操作仅在 Windows 有意义;非 Windows 上 LoadProfile/ListProfiles
// 直接返回错误,由 cli 走 fallback 路径。
package main

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"github.com/tobischo/gokeepasslib/v3"
)

// ---------- 路径/凭证默认值 ----------

// kdbxPath —— 可被 CCC_KDBX_PATH 覆盖(测试隔离钩子)。
func kdbxPath() string {
	if p := os.Getenv("CCC_KDBX_PATH"); p != "" {
		return p
	}
	return `E:\__work\BaseTools\cc-switch-kdbx\kdbx\apikey.kdbx`
}

// kdbxGroup —— 可被 CCC_KDBX_GROUP 覆盖。
func kdbxGroup() string {
	if g := os.Getenv("CCC_KDBX_GROUP"); g != "" {
		return g
	}
	return "cc"
}

// credPathFor —— DPAPI 凭据文件路径, 始终与 kdbx 同目录。
// 沿用 cc-switch-kdbx.kdbx_cred.cred_path() 约定
// (即同目录下的 .kdbx_master.cred)。
func credPathFor(kdbxPath string) string {
	return filepath.Join(filepath.Dir(kdbxPath), ".kdbx_master.cred")
}

// ---------- 字节级复刻: 骨架 + allow ----------

// skeletonEnv —— 与 compose_settings.py SKELETON_ENV 逐字相同,顺序也相同。
// 顺序决定序列化输出,改这里就破坏 Phase 4.1 验收。
var skeletonEnv = []struct{ K, V string }{
	{"CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC", "1"},
	{"DISABLE_TELEMETRY", "1"},
	{"CLAUDE_CODE_ATTRIBUTION_HEADER", "0"},
	{"CLAUDE_CODE_DISABLE_AUTO_MEMORY", "0"},
	{"CLAUDE_CODE_USE_POWERSHELL_TOOL", "1"},
	{"CLAUDE_CODE_MAX_CONTEXT_TOKENS", "1000000"},
	{"CLAUDE_CODE_AUTO_COMPACT_WINDOW", "500000"},
}

// allowList —— 与 compose_settings.py ALLOW 逐字相同。
var allowList = []string{
	"Bash", "Edit", "MultiEdit", "Write", "Read",
	"Glob", "Grep", "LS", "WebFetch", "WebSearch", "Rm",
}

// envEntry —— 内部 ordered (k,v) 对,保证序列化顺序。
type envEntry struct{ K, V string }

// ---------- 对外主入口: LoadProfile / ListProfiles ----------

// LoadProfile 从 kdbx 合成 profile 的 settings JSON 字节。
//
// 流程:
//   1. 读 CCC_KDBX_PATH (默认硬编码)
//   2. 读 .kdbx_master.cred 并 DPAPI 解密主密码 (用后立即清引用)
//   3. 解锁 kdbx
//   4. 找 cc 组
//   5. 遍历每条 entry 的 notes JSON,找 profiles{} 中 key=name 的那条
//   6. composeSettings + serializeSettings 返回字节
//
// 找不到 profile 名 → fmt.Errorf("profile not found in kdbx: %s", name)
func LoadProfile(name string) ([]byte, error) {
	if !isWindows() {
		return nil, errors.New("kdbx: Windows-only feature")
	}

	db, err := openAndUnlockKdbx()
	if err != nil {
		return nil, err
	}

	grpName := kdbxGroup()
	rootGroup := db.Content.Root
	g := findGroupInRoot(rootGroup, grpName)
	if g == nil {
		return nil, fmt.Errorf(
			"kdbx: group %q not found (set CCC_KDBX_GROUP to your group name)", grpName)
	}

	for i := range g.Entries {
		e := &g.Entries[i]
		notesText := e.GetContent("Notes")
		if notesText == "" {
			continue
		}
		var notes map[string]any
		if err := json.Unmarshal([]byte(notesText), &notes); err != nil {
			continue // 非法 JSON 跳过(非供应商记录)
		}
		profiles, ok := notes["profiles"].(map[string]any)
		if !ok {
			continue
		}
		if _, found := profiles[name]; !found {
			continue
		}
		// 命中: 合成 + 序列化
		token := e.GetPassword()
		env := composeSettings(notes, token, name)
		return serializeSettings(env), nil
	}
	return nil, fmt.Errorf("profile not found in kdbx: %s", name)
}

// ListProfiles 列出 kdbx cc 组中所有可解析的 profile 名(去重,字母序)。
// 非法 JSON notes 或非供应商记录 → 跳过,不出错。
func ListProfiles() ([]string, error) {
	names, _, err := ListProfilesRich()
	return names, err
}

// ProfileSource 描述一条 kdbx profile 的来源:名字 + 所在 entry title。
type ProfileSource struct {
	Name       string // profiles{} 中的 key (用户调用 ccc <name>)
	EntryTitle string // 所在 entry 的 title (用于 list 显示 "kdbx:cc/<entry>")
}

// ListProfilesRich 返回 (profile 名 → 所在 entry title) 的映射表;
// 单个 entry 多个 profile 时,共用同一 EntryTitle。
// 非法 JSON notes / 非供应商记录 → 跳过,不出错。
func ListProfilesRich() ([]string, []ProfileSource, error) {
	if !isWindows() {
		return nil, nil, errors.New("kdbx: Windows-only feature")
	}
	db, err := openAndUnlockKdbx()
	if err != nil {
		return nil, nil, err
	}
	grpName := kdbxGroup()
	g := findGroupInRoot(db.Content.Root, grpName)
	if g == nil {
		return nil, nil, fmt.Errorf(
			"kdbx: group %q not found (set CCC_KDBX_GROUP to your group name)", grpName)
	}

	seen := map[string]bool{}
	var names []string
	var sources []ProfileSource
	for i := range g.Entries {
		e := &g.Entries[i]
		notesText := e.GetContent("Notes")
		if notesText == "" {
			continue
		}
		var notes map[string]any
		if err := json.Unmarshal([]byte(notesText), &notes); err != nil {
			continue
		}
		profiles, ok := notes["profiles"].(map[string]any)
		if !ok {
			continue
		}
		entryTitle := e.GetTitle()
		for k := range profiles {
			if seen[k] {
				continue
			}
			seen[k] = true
			names = append(names, k)
			sources = append(sources, ProfileSource{Name: k, EntryTitle: entryTitle})
		}
	}
	// 字母序稳定输出(Go map 迭代随机);两个切片同步排序
	sortByName(names, sources)
	return names, sources, nil
}

// sortByName 同步按 names[i] 字母序排序 sources。
func sortByName(names []string, sources []ProfileSource) {
	for i := 1; i < len(names); i++ {
		for j := i; j > 0 && names[j-1] > names[j]; j-- {
			names[j-1], names[j] = names[j], names[j-1]
			sources[j-1], sources[j] = sources[j], sources[j-1]
		}
	}
}

// ---------- kdbx 打开 + 解锁 ----------

// openAndUnlockKdbx 读 DPAPI 凭据 → 打开 → 解锁 protected entries。
// 返回 *Database 指针;调用方读完即丢弃。
func openAndUnlockKdbx() (*gokeepasslib.Database, error) {
	path := kdbxPath()
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf(
				"kdbx file not found at %s (set CCC_KDBX_PATH)", path)
		}
		return nil, fmt.Errorf("kdbx stat: %w", err)
	}

	master, err := readCredDpapi(path)
	if err != nil {
		return nil, fmt.Errorf("kdbx master password: %w", err)
	}
	// 立即清引用。Go string 不可变,但起码阻止后续引用延长存活期。
	defer func() { master = "" }()

	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open kdbx: %w", err)
	}
	defer f.Close()

	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(master)
	dec := gokeepasslib.NewDecoder(f)
	if err := dec.Decode(db); err != nil {
		// gokeepasslib 在主密码错误时返回 "Database integrity check failed"
		// —— 包装为更明确的 bad credentials 提示
		if strings.Contains(err.Error(), "integrity check") {
			setupScript := filepath.Join(filepath.Dir(path), "cc_kdbx.ps1")
			return nil, fmt.Errorf(
				"kdbx: bad credentials (DPAPI blob at %s may be stale; "+
					"re-run: pwsh -File %q)",
				credPathFor(path), setupScript)
		}
		return nil, fmt.Errorf("kdbx decode: %w", err)
	}
	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("kdbx unlock: %w", err)
	}
	return db, nil
}

// findGroup 在组树中递归找 name 匹配的组。找不到返回 nil。
// 对应 ps1 的 find_group 语义。
func findGroup(g *gokeepasslib.Group, name string) *gokeepasslib.Group {
	if g == nil {
		return nil
	}
	if g.Name == name {
		return g
	}
	for i := range g.Groups {
		if found := findGroup(&g.Groups[i], name); found != nil {
			return found
		}
	}
	return nil
}

// findGroupInRoot 是 RootData 入口的便利包装。
func findGroupInRoot(root *gokeepasslib.RootData, name string) *gokeepasslib.Group {
	if root == nil {
		return nil
	}
	for i := range root.Groups {
		if found := findGroup(&root.Groups[i], name); found != nil {
			return found
		}
	}
	return nil
}

// ---------- 字节级合成: composeSettings + serializeSettings ----------

// composeSettings 1:1 移植 compose_settings.compose(notes, token, profile_name)。
// 返回 env 的有序条目(顺序 = Python dict 插入序,见 todolist Phase 4)。
//
// 输入 notes 必须是已通过 json.Unmarshal 的 dict;prof 必须是 profiles[name] 的 dict;
// 不再校验 schema(在 LoadProfile 入口已过滤)。
func composeSettings(notes map[string]any, token, profileName string) []envEntry {
	profiles := notes["profiles"].(map[string]any)
	prof := profiles[profileName].(map[string]any)
	model, _ := prof["model"].(string)

	var env []envEntry

	// 1. extra_env 最前(Python notes.get('extra_env') or {})
	if extra, ok := notes["extra_env"].(map[string]any); ok {
		for k, v := range extra {
			env = append(env, envEntry{K: k, V: toString(v)})
		}
	}

	// 2. ANTHROPIC_* 固定序
	env = append(env, envEntry{K: "ANTHROPIC_BASE_URL", V: toString(notes["base_url"])})
	env = append(env, envEntry{K: "ANTHROPIC_AUTH_TOKEN", V: token})
	env = append(env, envEntry{K: "ANTHROPIC_DEFAULT_HAIKU_MODEL", V: pickOr(prof, "haiku", model)})
	env = append(env, envEntry{K: "ANTHROPIC_DEFAULT_OPUS_MODEL", V: pickOr(prof, "opus", model)})
	env = append(env, envEntry{K: "ANTHROPIC_DEFAULT_SONNET_MODEL", V: pickOr(prof, "sonnet", model)})
	env = append(env, envEntry{K: "ANTHROPIC_MODEL", V: model})

	// 3. SKELETON_ENV 固定序
	for _, kv := range skeletonEnv {
		env = append(env, envEntry{K: kv.K, V: kv.V})
	}
	return env
}

// serializeSettings 1:1 移植 compose_settings.serialize(obj)。
// 字节级复刻:
//   - 2 空格缩进
//   - allow 数组单行紧凑("Bash","Edit",...)
//   - 顶层字段序: env, permissions, skipDangerousModePermissionPrompt
//   - 末尾 \n
//   - 不带 current_Model_Provider(todolist 明确去掉)
func serializeSettings(env []envEntry) []byte {
	var b strings.Builder

	b.WriteString("{\n")
	// env
	b.WriteString("  \"env\": {\n")
	for i, e := range env {
		if i > 0 {
			b.WriteString(",\n")
		}
		b.WriteString("    ")
		b.WriteString(strconv.Quote(e.K))
		b.WriteString(": ")
		b.WriteString(strconv.Quote(e.V))
	}
	b.WriteString("\n  },\n")
	// permissions
	b.WriteString("  \"permissions\": {\n")
	b.WriteString("    \"defaultMode\": \"bypassPermissions\",\n")
	b.WriteString("    \"allow\": [")
	for i, a := range allowList {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(strconv.Quote(a))
	}
	b.WriteString("]\n")
	b.WriteString("  },\n")
	// skipDangerousModePermissionPrompt
	b.WriteString("  \"skipDangerousModePermissionPrompt\": true\n")
	b.WriteString("}\n")
	return []byte(b.String())
}

// toString JSON-decoded 值都成 any;统一转 string。
// 数字/布尔: Python str() 等价;字符串: 原样。
func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case bool:
		if x {
			return "true"
		}
		return "false"
	case float64:
		// JSON 数字统一 float64;整数化(无小数部分)以避免 "1.0"
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	default:
		return fmt.Sprint(v)
	}
}

// pickOr 返回 prof[k](若存在且非空),否则 fallback。
func pickOr(prof map[string]any, k, fallback string) string {
	if v, ok := prof[k].(string); ok && v != "" {
		return v
	}
	return fallback
}

// ---------- Windows DPAPI (crypt32.CryptUnprotectData) ----------

// dataBlob —— Win32 DATA_BLOB 镜像。字段顺序和类型必须与 crypt32.h 一致。
//
//nolint:unused // 仅供 unsafe.Pointer 转换使用
type dataBlob struct {
	cbData uint32
	pbData *byte
}

// cryptUnprotectData 解密 DPAPI user-scope blob。
// 与 cc-switch-kdbx.kdbx_cred._crypt_unprotect 语义一致:
// pOptionalEntropy=NULL, pvReserved=NULL, pPromptStruct=NULL,
// dwFlags=0(当前用户范围;跨用户会失败)。
func cryptUnprotectData(ciphertext []byte) ([]byte, error) {
	if !isWindows() {
		return nil, errors.New("DPAPI requires Windows")
	}
	dll := syscall.NewLazyDLL("crypt32.dll")
	proc := dll.NewProc("CryptUnprotectData")
	k32 := syscall.NewLazyDLL("kernel32.dll")
	localFree := k32.NewProc("LocalFree")

	inBlob := &dataBlob{
		cbData: uint32(len(ciphertext)),
		pbData: &ciphertext[0],
	}
	var outBlob dataBlob

	r, _, e := proc.Call(
		uintptr(unsafe.Pointer(inBlob)),
		0, // ppszDataDescr
		0, // pOptionalEntropy
		0, // pvReserved
		0, // pPromptStruct
		0, // dwFlags
		uintptr(unsafe.Pointer(&outBlob)),
	)
	if r == 0 {
		return nil, fmt.Errorf("CryptUnprotectData: %v", e)
	}
	defer localFree.Call(uintptr(unsafe.Pointer(outBlob.pbData)))

	// 拷贝出去再返回,避免 LocalFree 后悬空。
	out := make([]byte, outBlob.cbData)
	copy(out, unsafe.Slice(outBlob.pbData, outBlob.cbData))
	return out, nil
}

// readCredDpapi 从 kdbx 同目录的 .kdbx_master.cred 读主密码明文。
// 文件 = base64(DPAPI ciphertext, current-user scope)。
// 与 cc-switch-kdbx.kdbx_cred.load() 等价一一对应。
//
// 错误信号:
//   - 文件不存在 → 提示运行同目录 cc_kdbx.ps1 重建
//   - CryptUnprotectData 失败 → 大概率是别的用户/机器保存的(DPAPI 绑 user+机器)
func readCredDpapi(kdbxPath string) (string, error) {
	if !isWindows() {
		return "", errors.New("kdbx credential read requires Windows")
	}
	credPath := credPathFor(kdbxPath)
	setupScript := filepath.Join(filepath.Dir(kdbxPath), "cc_kdbx.ps1")

	raw, err := os.ReadFile(credPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf(
				"credential file %q not found "+
					"(run: pwsh -File %q to create one)",
				credPath, setupScript)
		}
		return "", fmt.Errorf("read credential file: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return "", fmt.Errorf("credential file not valid base64: %w", err)
	}
	plaintext, err := cryptUnprotectData(ciphertext)
	if err != nil {
		return "", fmt.Errorf(
			"credential decrypt failed (likely wrong user or different machine; "+
				"re-run: pwsh -File %q): %w",
			setupScript, err)
	}
	return string(plaintext), nil
}