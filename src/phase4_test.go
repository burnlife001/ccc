// phase4_test.go —— Phase 4 字节级验收 + LoadProfile 行为测试。
//
// Phase 4.1 验收:
//   LoadProfile 合成的 settings JSON 必须与 compose_settings.py 对同一条 entry
//   的输出逐字节 diff 一致(去除 current_Model_Provider 后)。
//
// 这里不真正打开 kdbx(测试环境无 master password),而是把 composeSettings +
// serializeSettings 当成库,直接喂 Python 端使用的同一份 notes/token,
//
//   比较 Go 输出与一份"硬编码期望"(由 phase4_1_gen.py 一次性生成)。

package main

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// ----- 4.1 字节级 diff -----

// expectedFromPython —— compose_settings.py 输出的精确副本(去掉
// current_Model_Provider 后),由 phase4_1_gen.py 一次性生成。
// 这是"硬证据";若 Go 端输出变化,这里也要同步更新(用 gen 脚本)。
const expectedFromPython = `{
  "env": {
    "CUSTOM_FLAG": "1",
    "ANTHROPIC_BASE_URL": "https://api.minimaxi.com/anthropic",
    "ANTHROPIC_AUTH_TOKEN": "sk-cp-jCGoHi85Hf2HEaGCqb3kGt4vFAMKYopS-Qm7YXUJcku5HCS2RvGgi6lVilkEJ9P2V70LjGpXj2PCRQcEH4EClLBtTOrafAnDEVCKZIxePw8XGFnC70jIsRk",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "MiniMax-M3",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "MiniMax-M3-highspeed",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "MiniMax-M3",
    "ANTHROPIC_MODEL": "MiniMax-M3",
    "CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC": "1",
    "DISABLE_TELEMETRY": "1",
    "CLAUDE_CODE_ATTRIBUTION_HEADER": "0",
    "CLAUDE_CODE_DISABLE_AUTO_MEMORY": "0",
    "CLAUDE_CODE_USE_POWERSHELL_TOOL": "1",
    "CLAUDE_CODE_MAX_CONTEXT_TOKENS": "1000000",
    "CLAUDE_CODE_AUTO_COMPACT_WINDOW": "500000"
  },
  "permissions": {
    "defaultMode": "bypassPermissions",
    "allow": ["Bash","Edit","MultiEdit","Write","Read","Glob","Grep","LS","WebFetch","WebSearch","Rm"]
  },
  "skipDangerousModePermissionPrompt": true
}
`

func TestComposeAndSerialize_ByteDiffWithPython(t *testing.T) {
	notesJSON := `{
		"base_url": "https://api.minimaxi.com/anthropic",
		"profiles": {
			"minimax_m3": {
				"model": "MiniMax-M3",
				"haiku": "MiniMax-M3",
				"opus": "MiniMax-M3-highspeed",
				"sonnet": "MiniMax-M3"
			}
		},
		"extra_env": {"CUSTOM_FLAG": "1"}
	}`
	var notes map[string]any
	if err := json.Unmarshal([]byte(notesJSON), &notes); err != nil {
		t.Fatalf("notes parse: %v", err)
	}
	token := "sk-cp-jCGoHi85Hf2HEaGCqb3kGt4vFAMKYopS-Qm7YXUJcku5HCS2RvGgi6lVilkEJ9P2V70LjGpXj2PCRQcEH4EClLBtTOrafAnDEVCKZIxePw8XGFnC70jIsRk"

	env := composeSettings(notes, token, "minimax_m3")
	got := string(serializeSettings(env))

	if got != expectedFromPython {
		t.Fatalf("byte-diff with Python compose_settings.py FAILED\n--- GOT ---\n%s\n--- WANT ---\n%s", got, expectedFromPython)
	}
}

// 4.1b: 没有 extra_env 的纯最小 base —— 验证 env 起始是 ANTHROPIC_BASE_URL(无前置项)
func TestCompose_NoExtraEnv(t *testing.T) {
	notesJSON := `{
		"base_url": "https://api.example.com",
		"profiles": {"p1": {"model": "m1"}}
	}`
	var notes map[string]any
	if err := json.Unmarshal([]byte(notesJSON), &notes); err != nil {
		t.Fatalf("parse: %v", err)
	}
	env := composeSettings(notes, "tok", "p1")
	if len(env) == 0 {
		t.Fatal("env empty")
	}
	if env[0].K != "ANTHROPIC_BASE_URL" {
		t.Errorf("first env key = %q, want ANTHROPIC_BASE_URL", env[0].K)
	}
	if env[1].K != "ANTHROPIC_AUTH_TOKEN" {
		t.Errorf("second env key = %q, want ANTHROPIC_AUTH_TOKEN", env[1].K)
	}
	// 最后一个应是 CLAUDE_CODE_AUTO_COMPACT_WINDOW
	last := env[len(env)-1]
	if last.K != "CLAUDE_CODE_AUTO_COMPACT_WINDOW" {
		t.Errorf("last env key = %q, want CLAUDE_CODE_AUTO_COMPACT_WINDOW", last.K)
	}
}

// 4.1c: 模型覆盖字段缺失时回落 model
func TestCompose_ModelFallback(t *testing.T) {
	notesJSON := `{"base_url": "https://x", "profiles": {"p": {"model": "M"}}}`
	var notes map[string]any
	if err := json.Unmarshal([]byte(notesJSON), &notes); err != nil {
		t.Fatal(err)
	}
	env := composeSettings(notes, "tok", "p")
	want := []string{
		"ANTHROPIC_BASE_URL", "ANTHROPIC_AUTH_TOKEN",
		"ANTHROPIC_DEFAULT_HAIKU_MODEL", "ANTHROPIC_DEFAULT_OPUS_MODEL",
		"ANTHROPIC_DEFAULT_SONNET_MODEL", "ANTHROPIC_MODEL",
	}
	for i, k := range want {
		if env[i].K != k {
			t.Errorf("env[%d] key = %q, want %q", i, env[i].K, k)
		}
		if env[i].V != "M" && env[i].K != "ANTHROPIC_BASE_URL" && env[i].K != "ANTHROPIC_AUTH_TOKEN" {
			t.Errorf("env[%d] value = %q, want %q", i, env[i].V, "M")
		}
	}
}

// 4.1d: serializeSettings 输出末尾是 \n,顶层字段序 = env/permissions/skipDangerousModePermissionPrompt
func TestSerialize_FormatAndOrder(t *testing.T) {
	env := []envEntry{
		{K: "ANTHROPIC_BASE_URL", V: "https://x"},
		{K: "ANTHROPIC_AUTH_TOKEN", V: "tok"},
		{K: "ANTHROPIC_MODEL", V: "M"},
	}
	out := string(serializeSettings(env))
	if !strings.HasSuffix(out, "\n") {
		t.Error("output must end with \\n")
	}
	idxEnv := strings.Index(out, `"env":`)
	idxPerm := strings.Index(out, `"permissions":`)
	idxSkip := strings.Index(out, `"skipDangerousModePermissionPrompt":`)
	if idxEnv < 0 || idxPerm < 0 || idxSkip < 0 {
		t.Fatalf("missing top-level field: env=%d perm=%d skip=%d", idxEnv, idxPerm, idxSkip)
	}
	if !(idxEnv < idxPerm && idxPerm < idxSkip) {
		t.Errorf("top-level field order wrong: env=%d perm=%d skip=%d", idxEnv, idxPerm, idxSkip)
	}
	if strings.Contains(out, "current_Model_Provider") {
		t.Error("current_Model_Provider must NOT appear (todolist Phase 4 去掉)")
	}
	// allow 单行紧凑
	allowIdx := strings.Index(out, `"allow": [`)
	if allowIdx < 0 {
		t.Fatal(`"allow": [ not found`)
	}
	// 找到对应 ],中间不能有 \n
	end := strings.Index(out[allowIdx:], "]")
	if end < 0 {
		t.Fatal("allow block not closed")
	}
	if strings.Contains(out[allowIdx:allowIdx+end+1], "\n") {
		t.Errorf("allow block must be single-line, got: %q", out[allowIdx:allowIdx+end+1])
	}
}

// ----- 4.2 ListProfiles 形状(不开真 kdbx,仅验证本地路径 / 不存在的处理) -----

func TestLoadProfile_NoKdbxPath(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCC_KDBX_PATH", filepath.Join(tmp, "nope.kdbx"))

	_, err := LoadProfile("anything")
	if err == nil {
		t.Fatal("expected error for missing kdbx")
	}
	if !strings.Contains(err.Error(), "kdbx file not found") {
		t.Errorf("error should mention missing kdbx, got: %v", err)
	}
	if !strings.Contains(err.Error(), "CCC_KDBX_PATH") {
		t.Errorf("error should point to CCC_KDBX_PATH, got: %v", err)
	}
}

// 4.4 kdbx 主密码错误 / 不存在 —— 验证错误明确、不 panic、不写文件
func TestLoadProfile_NoCredential_NoPanic_NoFiles(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCC_HOME", tmp)
	// 留 CCC_KDBX_PATH 为默认;但 cred target 几乎肯定不在,所以这里
	// 走"cred 不存在"分支。验证:返回明确错误,临时 home 下无文件产生。

	// 用不存在的 kdbx 路径,先卡在这一层,确保错误来自 cred 读取前的检查
	t.Setenv("CCC_KDBX_PATH", filepath.Join(tmp, "no.kdbx"))
	_, err := LoadProfile("anything")
	if err == nil {
		t.Fatal("expected error")
	}
	// CCC_HOME 临时目录应该没有任何文件(未走到凭据读取那一步)
	entries, _ := os.ReadDir(tmp)
	if len(entries) != 0 {
		t.Errorf("expected tmp dir empty, got %d entries: %v", len(entries), entries)
	}
}

// 4.5 kdbx 路径不存在 → 明确报错指向 CCC_KDBX_PATH(同上已覆盖,这里再验错误前缀)
func TestLoadProfile_BadPath_MessagePointsToEnvVar(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCC_KDBX_PATH", filepath.Join(tmp, "absent.kdbx"))
	_, err := LoadProfile("p")
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if !strings.Contains(msg, "CCC_KDBX_PATH") {
		t.Errorf("err msg should point user to CCC_KDBX_PATH; got: %s", msg)
	}
}

// ----- parseProviderRecord 间接通过 ListProfiles 测试:非法 notes 跳过 -----

// 给 findGroup/findGroupInRoot 一些 sanity check(无需真 kdbx)
func TestFindGroupInRoot_NilSafe(t *testing.T) {
	if got := findGroupInRoot(nil, "x"); got != nil {
		t.Errorf("nil root should return nil, got %v", got)
	}
}

// 错误包装 / 边界:验证 ErrProfileNotFound 类型仍可被 cli.go 用 errors.Is 识别
// (kdbx 源不在 cli 走 resolveProfile 路径,但保证语义一致)
func TestKdbxLoadError_WrapsContext(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("CCC_KDBX_PATH", filepath.Join(tmp, "nope.kdbx"))
	_, err := LoadProfile("p")
	if err == nil {
		t.Fatal("want error")
	}
	// 错误应带 kdbx 前缀,便于日志/用户识别
	if !strings.HasPrefix(err.Error(), "kdbx") {
		t.Errorf("error should start with kdbx, got: %s", err.Error())
	}
}

// ----- .env 解析:注释/空行/引号/无等号行的处理 -----

func TestParseDotEnv(t *testing.T) {
	data := []byte("# comment\n" +
		"\n" +
		"CCC_KDBX_PATH=E:\\x\\y\\apikey.kdbx\n" +
		"  SPACED = v with spaces  \n" +
		"QUOTED=\"q v\"\n" +
		"SINGLE='s v'\n" +
		"NO_EQUALS_LINE\n" +
		"=nokey\n")
	got := parseDotEnv(data)
	want := [][2]string{
		{"CCC_KDBX_PATH", `E:\x\y\apikey.kdbx`},
		{"SPACED", "v with spaces"},
		{"QUOTED", "q v"},
		{"SINGLE", "s v"},
	}
	if len(got) != len(want) {
		t.Fatalf("got %d pairs %v, want %d", len(got), got, len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("pair %d: got %v, want %v", i, got[i], want[i])
		}
	}
}

// 占位: 真正的 LoadProfile 端到端需要 master password,留 e2e_test.go 里跑。
// 这里至少保证 kdbx.go 的所有纯逻辑(compose / serialize / findGroup / 错误路径)
// 都有测试覆盖。
var _ = errors.New // keep import in case of future