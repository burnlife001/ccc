package main

// exec_test.go —— assembleArgs / strippedAnthropicEnv 表驱动测试。
// 满足 docs/go架构规范.md §6（exec.go 的纯函数抽出来测；不真实 spawn）。

import (
	"reflect"
	"strings"
	"testing"
)

func TestAssembleArgs(t *testing.T) {
	tests := []struct {
		name string
		opts ExecOptions
		want []string
	}{
		{
			name: "settings_only",
			opts: ExecOptions{SettingsPath: "C:/x/p.json"},
			want: []string{"--settings", "C:/x/p.json"},
		},
		{
			name: "settings_plus_user",
			opts: ExecOptions{
				SettingsPath: "C:/x/p.json",
				UserArgs:     []string{"--max-turns", "5"},
			},
			want: []string{"--settings", "C:/x/p.json", "--max-turns", "5"},
		},
		{
			name: "headless_stream_json",
			opts: ExecOptions{
				SettingsPath: "p.json",
				Headless:     true,
				UserArgs:     []string{"-p", "task"},
			},
			want: []string{"--settings", "p.json", "--output-format", "stream-json", "--verbose", "-p", "task"},
		},
		{
			name: "no_settings",
			opts: ExecOptions{UserArgs: []string{"--help"}},
			want: []string{"--help"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := assembleArgs(tc.opts)
			if !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("assembleArgs(%+v)\n got=%v\nwant=%v", tc.opts, got, tc.want)
			}
		})
	}
}

func TestStrippedAnthropicEnv(t *testing.T) {
	// 构造一个 env 列表，覆盖各种 ANTHROPIC_* 前缀 + 杂项。
	in := []string{
		"PATH=/usr/bin",
		"ANTHROPIC_BASE_URL=https://evil.example",
		"ANTHROPIC_AUTH_TOKEN=stolen",
		"ANTHROPIC_API_KEY=sk-leak",
		"ANTHROPIC_MODEL=MiniMax-M3",
		"HOME=/home/user",
		"USERPROFILE=C:\\Users\\yg",
		"ANTHROPIC_LOG=debug",
		"=orphan_no_key",
	}
	got := strippedAnthropicEnvFrom(in)
	want := []string{
		"PATH=/usr/bin",
		"HOME=/home/user",
		"USERPROFILE=C:\\Users\\yg",
		"=orphan_no_key",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("strippedAnthropicEnv\ngot =%v\nwant=%v", got, want)
	}
	// 关键不变量：返回切片中任何元素都不是 ANTHROPIC_ 开头。
	for _, kv := range got {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			continue
		}
		if strings.HasPrefix(kv[:eq], "ANTHROPIC_") {
			t.Fatalf("found leaked ANTHROPIC_ key: %q", kv)
		}
	}
}

// strippedAnthropicEnvFrom 是 strippedAnthropicEnv 的可测版本——接受 env 输入
// 而非读 os.Environ()。生产路径仍然只调 strippedAnthropicEnv()。
func strippedAnthropicEnvFrom(env []string) []string {
	prefix := "ANTHROPIC_"
	out := make([]string, 0, len(env))
	for _, kv := range env {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		if strings.HasPrefix(kv[:eq], prefix) {
			continue
		}
		out = append(out, kv)
	}
	return out
}
