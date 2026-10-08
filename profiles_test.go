package main

// profiles_test.go —— validateProfileName / validateJSONBytes 表驱动测试。
// 满足 docs/go架构规范.md §6（paths/profiles 纯逻辑需表驱动测试）。

import (
	"strings"
	"testing"
)

func TestValidateProfileName(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty", "", true},
		{"plain", "minimax_m3", false},
		{"with_hyphen", "deepseek-pro", false},
		{"with_dot", "minimax.m3", true}, // 阻止隐藏 dot 文件名
		{"path_sep_slash", "foo/bar", true},
		{"path_sep_back", "foo\\bar", true},
		{"nul", "foo\x00bar", true},
		{"long", strings.Repeat("a", 200), false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProfileName(tc.input)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateProfileName(%q) err=%v wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}

func TestValidateJSONBytes(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		wantErr bool
	}{
		{"empty_object", `{}`, false},
		{"empty_array", `[]`, false},
		{"plain_string", `"hello"`, false},
		{"number", `42`, false},
		{"template_like", `{"env":{"ANTHROPIC_BASE_URL":"https://x"}}`, false},
		{"missing_close", `{"a":1`, true},
		{"trailing_comma", `{"a":1,}`, true},
		{"unquoted_key", `{a:1}`, true},
		{"empty", ``, true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateJSONBytes([]byte(tc.input))
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateJSONBytes(%q) err=%v wantErr=%v", tc.input, err, tc.wantErr)
			}
		})
	}
}
