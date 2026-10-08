package main

// phase3_test.go —— Phase 3 新增纯逻辑的表驱动测试。
// 覆盖 parseStreamJSON / saveSession / loadSession / arrangeHeadlessArgs / detectPrompt。

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ----- parseStreamJSON -----

func TestParseStreamJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantSid  string
		wantText string
	}{
		{
			name: "init_then_assistant",
			input: `{"type":"system","subtype":"init","session_id":"abc-123"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"OK"}]}}
`,
			wantSid:  "abc-123",
			wantText: "OK\n",
		},
		{
			name: "multiple_assistant_text_concatenated",
			input: `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}]}}
`,
			wantSid:  "s1",
			wantText: "hello world\n",
		},
		{
			name: "non_text_blocks_skipped",
			input: `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"},{"type":"text","text":"after tool"}]}}
`,
			wantSid:  "",
			wantText: "after tool\n",
		},
		{
			name: "result_fallback_when_no_assistant_text",
			input: `{"type":"system","subtype":"init","session_id":"s2"}
{"type":"result","is_error":false,"result":"final answer"}
`,
			wantSid:  "s2",
			wantText: "final answer\n",
		},
		{
			name: "result_ignored_when_assistant_already_texted",
			input: `{"type":"assistant","message":{"content":[{"type":"text","text":"primary"}]}}
{"type":"result","result":"should not override"}
`,
			wantSid:  "",
			wantText: "primary\n",
		},
		{
			name:     "session_id_from_result_event",
			input:    `{"type":"result","session_id":"from-result","result":"x"}`,
			wantSid:  "from-result",
			wantText: "x\n",
		},
		{
			name:     "unparseable_line_ignored",
			input:    "not json at all\n{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			wantSid:  "",
			wantText: "ok\n",
		},
		{
			name:     "empty",
			input:    "",
			wantSid:  "",
			wantText: "",
		},
		{
			name:     "blank_lines_skipped",
			input:    "\n\n\n{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"x\"}]}}\n\n",
			wantSid:  "",
			wantText: "x\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			sid, textBuf := parseStreamJSON([]byte(tc.input))
			if sid != tc.wantSid {
				t.Errorf("session_id = %q, want %q", sid, tc.wantSid)
			}
			if got := textBuf.String(); got != tc.wantText {
				t.Errorf("text = %q, want %q", got, tc.wantText)
			}
		})
	}
}

// ----- saveSession / loadSession -----

func TestSessionRoundtrip(t *testing.T) {
	// 用临时 HOME 隔离 ~/.ccc,避免污染真实数据
	tmpHome := t.TempDir()
	t.Setenv("CCC_HOME", tmpHome)

	if err := saveSession("minimax_m3", "sid-1"); err != nil {
		t.Fatalf("saveSession: %v", err)
	}
	if err := saveSession("kimi", "sid-2"); err != nil {
		t.Fatalf("saveSession: %v", err)
	}

	got1, _ := loadSession("minimax_m3")
	got2, _ := loadSession("kimi")
	if got1 != "sid-1" || got2 != "sid-2" {
		t.Errorf("roundtrip failed: got1=%q got2=%q", got1, got2)
	}

	// 更新已存在的 key(覆盖语义)
	if err := saveSession("minimax_m3", "sid-1-updated"); err != nil {
		t.Fatalf("saveSession update: %v", err)
	}
	got1, _ = loadSession("minimax_m3")
	if got1 != "sid-1-updated" {
		t.Errorf("update failed: got=%q", got1)
	}
	// 另一 profile 不受影响
	got2, _ = loadSession("kimi")
	if got2 != "sid-2" {
		t.Errorf("sibling polluted: got=%q", got2)
	}

	// 验证文件内容是合法 JSON 且包含两个 key
	data, _ := os.ReadFile(sessionsFile())
	var m map[string]string
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("sessions file invalid JSON: %v", err)
	}
	if len(m) != 2 {
		t.Errorf("expected 2 keys, got %d: %v", len(m), m)
	}
}

func TestLoadSessionMissing(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("CCC_HOME", tmpHome)

	got, err := loadSession("nope")
	if err != nil {
		t.Fatalf("loadSession missing should not error, got %v", err)
	}
	if got != "" {
		t.Errorf("missing profile should return empty, got %q", got)
	}
}

func TestLoadSessionCorruptJSON(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("CCC_HOME", tmpHome)

	if err := os.MkdirAll(cccDir(), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(sessionsFile(), []byte("not json {"), 0o600); err != nil {
		t.Fatalf("write corrupt: %v", err)
	}
	got, err := loadSession("anything")
	if err != nil {
		t.Errorf("corrupt JSON should be tolerated, got %v", err)
	}
	if got != "" {
		t.Errorf("corrupt JSON should yield empty, got %q", got)
	}
}

func TestSaveSessionEmptyID(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("CCC_HOME", tmpHome)

	// 空 session_id 是 no-op,不应该创建文件
	if err := saveSession("x", ""); err != nil {
		t.Errorf("empty session_id should not error, got %v", err)
	}
	if _, err := os.Stat(sessionsFile()); !os.IsNotExist(err) {
		t.Errorf("sessions file should not be created on empty id, stat err=%v", err)
	}
}

// ----- detectPrompt / arrangeHeadlessArgs / hasPromptFlag -----

func TestDetectPrompt(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		wantHL  bool
		wantIdx int
		wantErr bool
	}{
		{"none", []string{"--help"}, false, -1, false},
		{"dash_p", []string{"-p", "task"}, true, 0, false},
		{"dash_p_with_other_first", []string{"--max-turns", "5", "-p", "task"}, true, 2, false},
		{"dash_p_equals", []string{"-p=hi"}, true, 0, false},
		{"long_prompt", []string{"--prompt", "x"}, true, 0, false},
		{"dash_p_no_value", []string{"-p"}, false, -1, true},
		{"dash_p_last_no_value", []string{"a", "b", "-p"}, false, -1, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hl, idx, err := detectPrompt(tc.args)
			if (err != nil) != tc.wantErr {
				t.Fatalf("err=%v wantErr=%v", err, tc.wantErr)
			}
			if hl != tc.wantHL || idx != tc.wantIdx {
				t.Errorf("got hl=%v idx=%d, want hl=%v idx=%d", hl, idx, tc.wantHL, tc.wantIdx)
			}
		})
	}
}

func TestArrangeHeadlessArgs(t *testing.T) {
	tests := []struct {
		name string
		in   []string
		idx  int
		want []string
	}{
		{"no_prompt_idx", []string{"a", "b"}, -1, []string{"a", "b"}},
		{"dash_p_first", []string{"-p", "task", "--max-turns", "5"}, 0,
			[]string{"-p", "task", "--max-turns", "5"}},
		{"dash_p_middle", []string{"--max-turns", "5", "-p", "task"}, 2,
			[]string{"-p", "task", "--max-turns", "5"}},
		{"dash_p_equals", []string{"--max-turns", "5", "-p=hi"}, 2,
			[]string{"-p=hi", "--max-turns", "5"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := arrangeHeadlessArgs(tc.in, tc.idx)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("got %v, want %v", got, tc.want)
			}
		})
	}
}

func TestHasPromptFlag(t *testing.T) {
	if !hasPromptFlag([]string{"-p", "x"}) {
		t.Error("-p should be detected")
	}
	if !hasPromptFlag([]string{"--prompt", "x"}) {
		t.Error("--prompt should be detected")
	}
	if !hasPromptFlag([]string{"-p=hi"}) {
		t.Error("-p= should be detected")
	}
	if hasPromptFlag([]string{"--help"}) {
		t.Error("--help should not be a prompt flag")
	}
	if hasPromptFlag(nil) {
		t.Error("nil should not have prompt flag")
	}
}

// ----- sanity: 整个 sessions 文件路径推导在临时 HOME 下不会污染真实数据 -----

func TestSessionFileUnderTempHome(t *testing.T) {
	tmpHome := t.TempDir()
	t.Setenv("CCC_HOME", tmpHome)

	got := sessionsFile()
	want := filepath.Join(tmpHome, ".ccc", "sessions.json")
	if got != want {
		t.Errorf("sessionsFile = %q, want %q", got, want)
	}
	// 必须落在 tmpHome 下(临时隔离生效)
	rel, err := filepath.Rel(tmpHome, got)
	if err != nil || strings.HasPrefix(rel, "..") {
		t.Errorf("session path escaped tmpHome: got=%q rel=%q err=%v", got, rel, err)
	}
}
