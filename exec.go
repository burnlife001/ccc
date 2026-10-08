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
	Headless     bool     // true → 加 stream-json、解析 session_id
	Resume       bool     // true → 加 --resume <sessionId>(与 Headless 配套)
	SessionID    string   // --resume 的值
	UserArgs     []string // 用户透传参数,原样追加
}

// ExecResult 描述一次 headless 调用的产出。
type ExecResult struct {
	ExitCode  int
	SessionID string // 解析到的 session_id(若有)
	Text      string // 拼好的纯文本回复
}

// assembleArgs 是"参数组装"的纯函数,不发起 spawn。
// 任何修改 spawn 参数都先动这里 + 写表驱动测试。
//
// 约定:
//   - 调用方决定是否传 -p <prompt>(放 UserArgs 最前)。
//   - 本函数只负责"claude 启动旗标 + settings + stream-json + resume + 透传"。
//   - 旗标顺序与 headless-executor.ts 保持一致,便于交叉核对。
func assembleArgs(opts ExecOptions) []string {
	var args []string
	if opts.SettingsPath != "" {
		args = append(args, "--settings", opts.SettingsPath)
	}
	if opts.Headless {
		args = append(args, "--output-format", "stream-json", "--verbose")
	}
	if opts.Resume && opts.SessionID != "" {
		args = append(args, "--resume", opts.SessionID)
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

// spawnHeadless 用 stream-json 模式跑;收集 stdout 解析、stderr 直传用户。
// 退出码透传。session_id 由 cli 层决定是否落盘。
func spawnHeadless(resolve string, prefix, args []string) (ExecResult, error) {
	cmd := buildCmd(resolve, prefix, args)
	cmd.Stderr = os.Stderr       // 进度/报错直传,免得用户看不到 claude 原生错误
	cmd.Stdin = os.Stdin         // 保留,以防后续 pipe 输入
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
			return ExecResult{
				ExitCode: 1,
				Text:     string(stdoutAll),
			}, fmt.Errorf("spawn claude: %w", runErr)
		}
	}

	sessionID, textBuf := parseStreamJSON(stdoutAll)
	return ExecResult{
		ExitCode:  exitCode,
		SessionID: sessionID,
		Text:      textBuf.String(),
	}, nil
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
	Session string          `json:"session_id"`
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

// parseStreamJSON 逐行解析;抓首个 session_id,拼装所有 assistant text。
// 当 assistant 没有任何 text 输出时,回退到 result.result。
func parseStreamJSON(data []byte) (sessionID string, text bytes.Buffer) {
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
		if sessionID == "" && ev.Session != "" {
			sessionID = ev.Session
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
	return sessionID, text
}

// isWindows —— 通过路径分隔符判断(避免直接依赖 runtime.GOOS 文本)。
func isWindows() bool {
	return os.PathSeparator == '\\' && os.PathListSeparator == ';'
}

// saveSession 写入 ~/.ccc/sessions.json { profileName: sessionID }。
// 目录不存在时创建。原子写入(写 .tmp 后 rename)。
// 文件不存在或为空时初始化为 {};JSON 损坏时整体替换(止损)。
func saveSession(profile, sessionID string) error {
	if sessionID == "" {
		return nil
	}
	if err := ensureCccDirs(); err != nil {
		return err
	}
	p := sessionsFile()
	m := map[string]string{}
	data, err := os.ReadFile(p)
	switch {
	case err == nil && len(data) > 0:
		if jerr := json.Unmarshal(data, &m); jerr != nil {
			_ = os.Rename(p, p+".corrupt")
			m = map[string]string{}
		}
	case err != nil && !errors.Is(err, os.ErrNotExist):
		return err
	}
	m[profile] = sessionID
	out, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, out, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

// loadSession 取 profile 的 session_id;不存在或文件损坏返回空串。
func loadSession(profile string) (string, error) {
	p := sessionsFile()
	data, err := os.ReadFile(p)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", nil
		}
		return "", err
	}
	if len(data) == 0 {
		return "", nil
	}
	m := map[string]string{}
	if err := json.Unmarshal(data, &m); err != nil {
		return "", nil // 损坏视作空
	}
	return m[profile], nil
}
