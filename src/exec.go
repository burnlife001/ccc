package main

// exec.go —— 唯一允许 import os/exec 的文件。
// 职责:查找 claude、剥离 ANTHROPIC_*、spawn、stdio 接线、解析 stream-json。

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// ExecOptions 描述一次 claude 调用的全部参数组装上下文。
// 抽出来便于纯函数测试(go架构规范 §6)。
type ExecOptions struct {
	SettingsPath string   // --settings 的绝对路径
	Headless     bool     // true → 加 stream-json、解析 text
	UserArgs     []string // 用户透传参数,原样追加
}

// ExecResult 描述一次 headless 调用的产出。
type ExecResult struct {
	ExitCode int
	Text     string // 拼好的纯文本回复
}

// assembleArgs 是"参数组装"的纯函数,不发起 spawn。
// 任何修改 spawn 参数都先动这里 + 写表驱动测试。
//
// 约定:
//   - 调用方决定是否传 -p <prompt>(放 UserArgs 最前)。
//   - 本函数只负责"claude 启动旗标 + settings + stream-json + 透传"。
//   - 旗标顺序与 headless-executor.ts 保持一致,便于交叉核对。
func assembleArgs(opts ExecOptions) []string {
	var args []string
	if opts.SettingsPath != "" {
		args = append(args, "--settings", opts.SettingsPath)
	}
	if opts.Headless {
		args = append(args, "--output-format", "stream-json", "--verbose")
	}
	args = append(args, opts.UserArgs...)
	return args
}

// findClaude 解析 claude 命令;Windows 上 .cmd/.bat 走 cmd.exe 包裹,.ps1 走 powershell -File。
// 直接 .exe / 无后缀直接 exec。
//
// 返回两个值:
//   - resolve:用于 exec.Command 的可执行文件路径(Windows 上是 cmd.exe / powershell.exe)
//   - prefix: 用于 spawn 的前缀参数(.cmd → ["cmd.exe","/c",claude];.ps1 → powershell 参数)
//   - 直接 exe 时 prefix 为空
//
// 查找失败返回 error,不会 fallback 到 PATH(避免歧义)。
func findClaude() (resolve string, prefix []string, err error) {
	name := os.Getenv("CCC_CLAUDE")
	if name == "" {
		name = "claude"
	}
	resolved, err := exec.LookPath(name)
	if err != nil {
		return "", nil, fmt.Errorf("claude CLI not found in PATH (looked up %q). "+
			"Install: https://docs.claude.com/en/docs/claude-code/installation", name)
	}
	if isWindows() {
		lower := strings.ToLower(resolved)
		switch {
		case strings.HasSuffix(lower, ".ps1"):
			ps, perr := exec.LookPath("powershell.exe")
			if perr != nil {
				return "", nil, errors.New("powershell.exe not found (required for .ps1 claude wrapper)")
			}
			return ps, []string{"-NoProfile", "-ExecutionPolicy", "Bypass", "-File", resolved}, nil
		case strings.HasSuffix(lower, ".cmd"), strings.HasSuffix(lower, ".bat"):
			cmd, perr := exec.LookPath("cmd.exe")
			if perr != nil {
				return "", nil, errors.New("cmd.exe not found (required for .cmd/.bat claude wrapper)")
			}
			return cmd, []string{"/c", resolved}, nil
		}
	}
	return resolved, nil, nil
}

// strippedAnthropicEnv 返回剥掉 ANTHROPIC_* 与 CLAUDECODE 后的环境副本。
// Windows env 大小写不敏感;逐 key 大写比较以稳妥处理。
func strippedAnthropicEnv() []string {
	const prefix = "ANTHROPIC_"
	drop := map[string]bool{"CLAUDECODE": true}
	out := make([]string, 0, len(os.Environ()))
	for _, kv := range os.Environ() {
		eq := strings.IndexByte(kv, '=')
		if eq < 0 {
			out = append(out, kv)
			continue
		}
		k := kv[:eq]
		ku := strings.ToUpper(k)
		if strings.HasPrefix(ku, prefix) {
			continue
		}
		if drop[ku] {
			continue
		}
		out = append(out, kv)
	}
	return out
}

// spawnInteractive 全 stdio inherit —— 交互模式用。
// 退出码通过 os.Exit 透传给调用方(避免错误返回再被 main 多包一层)。
func spawnInteractive(resolve string, prefix, args []string) error {
	cmd := buildCmd(resolve, prefix, args)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = strippedAnthropicEnv()
	if err := cmd.Run(); err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			os.Exit(ee.ExitCode())
		}
		return fmt.Errorf("spawn claude: %w", err)
	}
	return nil
}

// spawnHeadless 用 stream-json 模式跑;收集 stdout 解析、stderr 过滤后直传。
// 退出码透传。session_id 由 cli 层决定是否落盘。
func spawnHeadless(resolve string, prefix, args []string) (ExecResult, error) {
	cmd := buildCmd(resolve, prefix, args)
	// 收 stderr,等子进程退出后过滤再回写,避免和 stream-json 输出交杂
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf
	cmd.Stdin = os.Stdin // 保留,以防后续 pipe 输入
	cmd.Env = strippedAnthropicEnv()

	// cmd.Output() 内部用 os.Pipe + 标准库 goroutine 拷贝,
	// 不算我们的 goroutine,符合架构规范。
	stdoutAll, runErr := cmd.Output()

	exitCode := 0
	if runErr != nil {
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			exitCode = ee.ExitCode()
		} else {
			// spawn 失败(claude 没起来),Output() 仍可能返回部分输出
			flushStderr(&stderrBuf)
			return ExecResult{
				ExitCode: 1,
				Text:     string(stdoutAll),
			}, fmt.Errorf("spawn claude: %w", runErr)
		}
	}

	flushStderr(&stderrBuf)
	textBuf := parseStreamJSON(stdoutAll)
	return ExecResult{
		ExitCode: exitCode,
		Text:     textBuf.String(),
	}, nil
}

// flushStderr 把子进程 stderr 回写给用户;逐行过滤已知无害警告。
// 已知要过滤:[claude-code:unrecognized_model] — 自定义 model 名触发的无害告警,
// 重复刷屏但不影响调用。其它行原样回写(用户该看到的错误还得看到)。
func flushStderr(buf *bytes.Buffer) {
	if buf.Len() == 0 {
		return
	}
	scanner := bufio.NewScanner(buf)
	scanner.Buffer(make([]byte, 0, 64*1024), 64*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "[claude-code:unrecognized_model]") {
			continue
		}
		fmt.Fprintln(os.Stderr, line)
	}
}

// buildCmd 拼接 exec.Cmd,prefix 与 args 拼在 resolve 之后。
func buildCmd(resolve string, prefix, args []string) *exec.Cmd {
	if len(prefix) > 0 {
		all := make([]string, 0, len(prefix)+len(args))
		all = append(all, prefix...)
		all = append(all, args...)
		return exec.Command(resolve, all...)
	}
	return exec.Command(resolve, args...)
}

// streamEvent 是 stream-json 输出行最小子集。
// 完整 schema 远超此处;只取我们要的字段。
type streamEvent struct {
	Type    string          `json:"type"`
	Subtype string          `json:"subtype"`
	Message json.RawMessage `json:"message,omitempty"`
}

// assistantText 从 assistant.message.content[].text 抽取所有 text 块,按出现顺序拼接。
func assistantText(msgRaw json.RawMessage) string {
	if len(msgRaw) == 0 {
		return ""
	}
	var msg struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(msgRaw, &msg); err != nil {
		return ""
	}
	var sb strings.Builder
	for _, c := range msg.Content {
		if c.Type == "text" {
			sb.WriteString(c.Text)
		}
	}
	return sb.String()
}

// parseStreamJSON 逐行解析;拼装所有 assistant text。
// 当 assistant 没有任何 text 输出时,回退到 result.result。
func parseStreamJSON(data []byte) (text bytes.Buffer) {
	scanner := bufio.NewScanner(bytes.NewReader(data))
	// 单行可能很大(一次 assistant 全部 content);放宽上限到 8MB。
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}
		var ev streamEvent
		if err := json.Unmarshal(line, &ev); err != nil {
			continue // 不可解析行忽略(可能含进度前缀等),不致命
		}
		switch ev.Type {
		case "assistant":
			if t := assistantText(ev.Message); t != "" {
				text.WriteString(t)
				text.WriteByte('\n')
			}
		case "result":
			if text.Len() == 0 {
				var r struct {
					Result string `json:"result"`
				}
				if err := json.Unmarshal(line, &r); err == nil && r.Result != "" {
					text.WriteString(r.Result)
					text.WriteByte('\n')
				}
			}
		}
	}
	return text
}

// isWindows —— 通过路径分隔符判断(避免直接依赖 runtime.GOOS 文本)。
func isWindows() bool {
	return os.PathSeparator == '\\' && os.PathListSeparator == ';'
}
