package main

// phase3_test.go —— Phase 3 纯逻辑的表驱动测试。
// 覆盖 parseStreamJSON / arrangeHeadlessArgs / detectPrompt。

import (
	"reflect"
	"testing"
)

// ----- parseStreamJSON -----

func TestParseStreamJSON(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		wantText string
	}{
		{
			name: "init_then_assistant",
			input: `{"type":"system","subtype":"init","session_id":"abc-123"}
{"type":"assistant","message":{"role":"assistant","content":[{"type":"text","text":"OK"}]}}
`,
			wantText: "OK\n",
		},
		{
			name: "multiple_assistant_text_concatenated",
			input: `{"type":"system","subtype":"init","session_id":"s1"}
{"type":"assistant","message":{"content":[{"type":"text","text":"hello "},{"type":"text","text":"world"}]}}
`,
			wantText: "hello world\n",
		},
		{
			name: "non_text_blocks_skipped",
			input: `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash"},{"type":"text","text":"after tool"}]}}
`,
			wantText: "after tool\n",
		},
		{
			name: "result_fallback_when_no_assistant_text",
			input: `{"type":"system","subtype":"init","session_id":"s2"}
{"type":"result","is_error":false,"result":"final answer"}
`,
			wantText: "final answer\n",
		},
		{
			name: "result_ignored_when_assistant_already_texted",
			input: `{"type":"assistant","message":{"content":[{"type":"text","text":"primary"}]}}
{"type":"result","result":"should not override"}
`,
			wantText: "primary\n",
		},
		{
			name:     "unparseable_line_ignored",
			input:    "not json at all\n{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"ok\"}]}}\n",
			wantText: "ok\n",
		},
		{
			name:     "empty",
			input:    "",
			wantText: "",
		},
		{
			name:     "blank_lines_skipped",
			input:    "\n\n\n{\"type\":\"assistant\",\"message\":{\"content\":[{\"type\":\"text\",\"text\":\"x\"}]}}\n\n",
			wantText: "x\n",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			textBuf := parseStreamJSON([]byte(tc.input))
			if got := textBuf.String(); got != tc.wantText {
				t.Errorf("text = %q, want %q", got, tc.wantText)
			}
		})
	}
}

// ----- detectPrompt / arrangeHeadlessArgs -----

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
