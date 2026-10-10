// ccc —— kdbx 直读 + 内存合成 settings JSON + DPAPI 凭据存取。
// 唯一允许 import gokeepasslib / syscall.crypt32 的文件;
//
// 阶段路线 (todolist Phase 4 + Phase 5):
//   1. cryptProtectData / cryptUnprotectData: DPAPI user-scope 加解密
//   2. storeCred / checkCred / deleteCred: 对应 `ccc cred store|check|delete`
//   3. readCredDpapi: 读 .kdbx_master.cred 并 CryptUnprotectData
//   4. openAndUnlockKdbx: 解锁 kdbx 并解锁 protected entries
//   5. findGroup: 递归找 cc 组
//   6. composeSettings + serializeSettings: 1:1 复刻 compose_settings.py
//   7. LoadProfile / ListProfiles: 对外 API
//
// 字节级复刻原则 (Phase 4.1 验收):
//   - 顶层字段序: env, permissions, skipDangerousModePermissionPrompt
//   - env 序: extra_env → ANTHROPIC_BASE_URL → AUTH_TOKEN → DEFAULT_*_MODEL → MODEL → SKELETON_ENV
//   - allow 数组单行紧凑 ("Bash","Edit",...)
//   - 2 空格缩进,末尾 \n, 不带 current_Model_Provider
//
// 跨平台: kdbx / DPAPI 仅在 Windows 有意义;非 Windows 上 LoadProfile/ListProfiles
// 及 store/check/delete 均直接返回错误,由 cli 报错退出。
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

// kdbxPath —— 只认 CCC_KDBX_PATH(由 <exeDir>/.env 或环境变量提供)。
// 源码不硬编码任何机器路径;未设置时返回空串,由 openAndUnlockKdbx 报错。
// 相对路径相对 <exeDir> 解析(而非进程 CWD),整个项目目录可随意搬动。
func kdbxPath() string {
	p := os.Getenv("CCC_KDBX_PATH")
	if p != "" && !filepath.IsAbs(p) {
		p = filepath.Join(exeDir(), p)
	}
	return p
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
	if path == "" {
		return nil, fmt.Errorf(
			"kdbx: CCC_KDBX_PATH not set "+
				"(add it to %s or the environment)",
			filepath.Join(exeDir(), ".env"))
	}
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
			return nil, fmt.Errorf(
				"kdbx: bad credentials (DPAPI blob at %s may be stale; "+
					"re-run: ccc cred store)",
				credPathFor(path))
		}
		return nil, fmt.Errorf("kdbx decode: %w", err)
	}
	if err := db.UnlockProtectedEntries(); err != nil {
		return nil, fmt.Errorf("kdbx unlock: %w", err)
	}
	return db, nil
}

// verifyKdbxPassword 用候选密码试开 kdbx;成功 → nil,失败 → 错误。
// 失败情形:
//   - 主密码错 → "kdbx: bad credentials ..."
//   - 文件 IO / 解析 → 透传
// 共享给 storeCred (提交前预演) 和 checkCred (持证复核)。
func verifyKdbxPassword(kdbxPath, password string) error {
	f, err := os.Open(kdbxPath)
	if err != nil {
		return fmt.Errorf("open kdbx: %w", err)
	}
	defer f.Close()

	db := gokeepasslib.NewDatabase()
	db.Credentials = gokeepasslib.NewPasswordCredentials(password)
	if err := gokeepasslib.NewDecoder(f).Decode(db); err != nil {
		if strings.Contains(err.Error(), "integrity check") {
			return fmt.Errorf(
				"kdbx: bad credentials (password does NOT unlock the kdbx)")
		}
		return fmt.Errorf("kdbx decode: %w", err)
	}
	return nil
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

	// 空切片取址会 panic;给 nil 指针,cbData=0。
	var pb *byte
	if len(ciphertext) > 0 {
		pb = &ciphertext[0]
	}
	inBlob := &dataBlob{
		cbData: uint32(len(ciphertext)),
		pbData: pb,
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
	if outBlob.cbData > 0 {
		copy(out, unsafe.Slice(outBlob.pbData, outBlob.cbData))
	}
	return out, nil
}

// cryptProtectData 加密 DPAPI user-scope blob。
// 与 cryptUnprotectData 对称,供 storeCred 使用;
// 输出格式与 cc-switch-kdbx.kdbx_cred._crypt_protect 等价,字节级可互换。
//
// 参数与 cryptUnprotectData 相同:
// pOptionalEntropy=NULL, pvReserved=NULL, pPromptStruct=NULL,
// dwFlags=0(当前用户范围)。
func cryptProtectData(plaintext []byte) ([]byte, error) {
	if !isWindows() {
		return nil, errors.New("DPAPI requires Windows")
	}
	dll := syscall.NewLazyDLL("crypt32.dll")
	proc := dll.NewProc("CryptProtectData")
	k32 := syscall.NewLazyDLL("kernel32.dll")
	localFree := k32.NewProc("LocalFree")

	var pb *byte
	if len(plaintext) > 0 {
		pb = &plaintext[0]
	}
	inBlob := &dataBlob{
		cbData: uint32(len(plaintext)),
		pbData: pb,
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
		return nil, fmt.Errorf("CryptProtectData: %v", e)
	}
	defer localFree.Call(uintptr(unsafe.Pointer(outBlob.pbData)))

	out := make([]byte, outBlob.cbData)
	if outBlob.cbData > 0 {
		copy(out, unsafe.Slice(outBlob.pbData, outBlob.cbData))
	}
	return out, nil
}

// readCredDpapi 从 kdbx 同目录的 .kdbx_master.cred 读主密码明文。
// 文件 = base64(DPAPI ciphertext, current-user scope)。
// 与 cc-switch-kdbx.kdbx_cred.load() 等价一一对应。
//
// 错误信号:
//   - 文件不存在 → 提示运行 ccc cred store 重建
//   - CryptUnprotectData 失败 → 大概率是别的用户/机器保存的(DPAPI 绑 user+机器)
func readCredDpapi(kdbxPath string) (string, error) {
	if !isWindows() {
		return "", errors.New("kdbx credential read requires Windows")
	}
	credPath := credPathFor(kdbxPath)

	raw, err := os.ReadFile(credPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf(
				"credential file %q not found "+
					"(run: ccc cred store to create one)",
				credPath)
		}
		return "", fmt.Errorf("read credential file: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return "", fmt.Errorf(
			"credential file not valid base64 "+
				"(corrupt? run: ccc cred store to recreate): %w", err)
	}
	plaintext, err := cryptUnprotectData(ciphertext)
	if err != nil {
		return "", fmt.Errorf(
			"credential decrypt failed (likely wrong user or different machine; "+
				"run: ccc cred store): %w", err)
	}
	return string(plaintext), nil
}

// ---------- DPAPI 凭据管理 (ccc cred store|check|delete) ----------

// storeCred: 先 verify_kdbx 校验密码,再 CryptProtectData 加密 → base64 落盘。
// 步骤:
//   1. 候选密码试开 kdbx,失败直接返回(不落盘)—— 满足 A3 验收
//   2. DPAPI 加密,写 base64(ciphertext) 到 credPathFor(kdbxPath)
//   3. 文件权限 0600(其它进程无法读取)
// 落盘前任何步骤失败 → 不产生 cred 文件(原子化保证)。
func storeCred(kdbxPath, password string) (retErr error) {
	if !isWindows() {
		return errors.New("cred store requires Windows")
	}
	if err := verifyKdbxPassword(kdbxPath, password); err != nil {
		return err
	}
	ciphertext, err := cryptProtectData([]byte(password))
	if err != nil {
		return fmt.Errorf("DPAPI encrypt: %w", err)
	}
	encoded := base64.StdEncoding.EncodeToString(ciphertext)
	credPath := credPathFor(kdbxPath)

	// 落盘 + rename 到最终位置 → 写到同目录临时文件再 rename,避免半截文件
	dir := filepath.Dir(credPath)
	tmp, err := os.CreateTemp(dir, ".kdbx_master.cred.*.tmp")
	if err != nil {
		return fmt.Errorf("create temp cred file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() {
		if retErr != nil {
			_ = os.Remove(tmpName) // 失败时清残留
		}
	}()
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("chmod cred: %w", err)
	}
	if _, err := tmp.WriteString(encoded); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("write cred: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close cred: %w", err)
	}
	if err := os.Rename(tmpName, credPath); err != nil {
		return fmt.Errorf("rename cred: %w", err)
	}
	return nil
}

// checkCred: 读 cred → DPAPI 解密 → 试开 kdbx;全部通过 → nil。
// 失败原因:
//   - 文件不存在 → 指向 ccc cred store
//   - base64 不合法 → 指向 ccc cred store(腐坏)
//   - DPAPI 解密失败 → 跨用户/跨机器(DPAPI 绑 user+机器),指向 ccc cred store
//   - 解出来的主密码打不开 kdbx → 指向 ccc cred store(stale)
func checkCred(kdbxPath string) error {
	if !isWindows() {
		return errors.New("cred check requires Windows")
	}
	credPath := credPathFor(kdbxPath)
	raw, err := os.ReadFile(credPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf(
				"credential file %q not found "+
					"(run: ccc cred store to create one)", credPath)
		}
		return fmt.Errorf("read credential file: %w", err)
	}
	ciphertext, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(raw)))
	if err != nil {
		return fmt.Errorf(
			"credential file not valid base64 "+
				"(corrupt? run: ccc cred store to recreate): %w", err)
	}
	plaintext, err := cryptUnprotectData(ciphertext)
	if err != nil {
		return fmt.Errorf(
			"credential decrypt failed (likely wrong user or different machine; "+
				"run: ccc cred store): %w", err)
	}
	pw := string(plaintext)
	// 立即清引用(防 panic 后仍可被 recover 利用)
	defer func() { pw = ""; plaintext = nil }()

	if err := verifyKdbxPassword(kdbxPath, pw); err != nil {
		return fmt.Errorf(
			"credential decrypts but does NOT open the kdbx "+
				"(password changed? run: ccc cred store): %w", err)
	}
	return nil
}

// deleteCred: 删 cred 文件;不存在时返回 (existed=false, err=nil),供调用方友好提示。
func deleteCred(kdbxPath string) (existed bool, err error) {
	if !isWindows() {
		return false, errors.New("cred delete requires Windows")
	}
	credPath := credPathFor(kdbxPath)
	if rerr := os.Remove(credPath); rerr != nil {
		if errors.Is(rerr, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("remove cred: %w", rerr)
	}
	return true, nil
}

// ---------- 交互式密码读取 (stdin tty only) ----------

// Windows console 旗标常量。来源:Win32 Console Modes。
const (
	enableEchoInput      uint32 = 0x0004
	enableProcessedInput uint32 = 0x0001
)

// readPasswordInteractive 从 console 逐字符读取密码,无回显,每字符打 '*'。
//   - 必须 stdin 是 console(否则立即报错,防管道/后台卡死)
//   - Enter(\r) → 终止
//   - Backspace(\b/\x7f) → 弹栈 + 屏幕 \b \b 退格
//   - Ctrl+C(\x03) → 返回错误,main.go 的 recover 会兜成 exit 2
//   - 函数键前缀(\x00/\xe0) → 吞掉下一个扫描码
//
// 实现全部走 stdlib syscall:kernel32!GetConsoleMode + 走 lazy DLL 直接调
// SetConsoleMode(syscall 未导出该函数) + Read。
// 不引入 golang.org/x/term 是为了严守 go架构规范 §7 的 std-only 原则。
func readPasswordInteractive(prompt string) (string, error) {
	h := syscall.Handle(os.Stdin.Fd())
	var oldMode uint32
	if err := syscall.GetConsoleMode(h, &oldMode); err != nil {
		// 非 console(管道/重定向)→ 直接失败,不阻塞
		return "", errors.New("requires interactive terminal (stdin is not a console)")
	}

	// syscall 包只导出 GetConsoleMode,SetConsoleMode 走 lazy DLL 直调。
	k32 := syscall.NewLazyDLL("kernel32.dll")
	setModeProc := k32.NewProc("SetConsoleMode")
	newMode := oldMode &^ enableEchoInput
	if r, _, e := setModeProc.Call(uintptr(h), uintptr(newMode)); r == 0 {
		return "", fmt.Errorf("disable console echo: %v", e)
	}
	defer func() {
		// 任何出口都恢复(成功 / 错误 / panic 都被 defer 捕获)
		_, _, _ = setModeProc.Call(uintptr(h), uintptr(oldMode))
	}()

	fmt.Fprint(os.Stderr, prompt)
	defer fmt.Fprintln(os.Stderr) // 末尾换行,保证 prompt 不挂在光标上

	var buf []byte
	ch := make([]byte, 1)
	for {
		n, err := syscall.Read(h, ch)
		if err != nil {
			return string(buf), fmt.Errorf("read password: %w", err)
		}
		if n == 0 {
			// EOF:用户 Ctrl+Z / 关 stdin;视为空密码
			return string(buf), nil
		}
		c := ch[0]
		switch c {
		case '\r', '\n':
			return string(buf), nil
		case '\x03':
			return "", errors.New("interrupted by Ctrl+C")
		case '\b', 0x7f:
			if len(buf) > 0 {
				buf = buf[:len(buf)-1]
				fmt.Fprint(os.Stderr, "\b \b")
			}
		default:
			// 吞掉功能键前缀字节(方向键、F1-F12 等发 0x00 或 0xe0 加扫描码)
			if c == 0x00 || c == 0xe0 {
				_, _ = syscall.Read(h, ch) // 二次读:忽略扫描码
				continue
			}
			// 过滤其它控制字符(防止意外的 ANSI 等)
			if c < 0x20 {
				continue
			}
			buf = append(buf, c)
			fmt.Fprint(os.Stderr, "*")
		}
	}
}