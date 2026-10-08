package main

// cli.go —— 命令分发:version/list/add/rm/which/<profile>/<profile>:continue。
// 只组合 profiles/exec/paths 暴露的函数,不引入新逻辑。

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// Dispatch 是 cli 的唯一对外入口。main 把 os.Args[1:] 传进来。
// 返回 exit code:0 成功;非 0 失败。
func Dispatch(args []string) int {
	if len(args) == 0 {
		printUsage(os.Stderr)
		return 2
	}

	// 子命令优先
	switch args[0] {
	case "version", "-v", "--version":
		fmt.Printf("ccc %s\n", version)
		return 0
	case "-h", "--help", "help":
		printUsage(os.Stdout)
		return 0
	case "list", "ls":
		return cmdList()
	case "which":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "ccc: usage: ccc which <profile> [args...]")
			return 2
		}
		return cmdWhich(args[1], args[2:])
	case "add":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "ccc: usage: ccc add <name>")
			return 2
		}
		return cmdAdd(args[1])
	case "rm", "remove":
		if len(args) < 2 {
			fmt.Fprintln(os.Stderr, "ccc: usage: ccc rm <name>")
			return 2
		}
		return cmdRm(args[1])
	}

	// 默认:<profile> 或 <profile>:continue
	return cmdProfile(args)
}

// runOrExit 是错误处理统一出口:错误则打印并返回非零 code。
// 错误消息按惯例:用户输入类错误在源站已带 "ccc: " 前缀,系统类错误不带;
// 这里原样打印,不再二次前缀,避免 "ccc: ccc: ..."。
func runOrExit(fn func() error) int {
	if err := fn(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, ErrProfileNotFound) {
			return 3
		}
		return 1
	}
	return 0
}

// ----- 子命令实现 -----

func cmdList() int {
	return runOrExit(func() error {
		names, err := listProfileNames()
		if err != nil {
			return err
		}
		if len(names) == 0 {
			fmt.Fprintln(os.Stderr, "(no profiles installed)")
			fmt.Fprintln(os.Stderr, "  bootstrap: copy ~/.ccs/settings_*.json → ~/.ccc/profiles/<name>.json")
			fmt.Fprintln(os.Stderr, "  or:        ccs add <name>")
			return nil
		}
		for _, n := range names {
			fmt.Printf("%s → %s\n", n, profilePath(n))
		}
		return nil
	})
}

func cmdWhich(profile string, userArgs []string) int {
	return runOrExit(func() error {
		p, err := resolveProfile(profile)
		if err != nil {
			return err
		}
		resolve, prefix, err := findClaude()
		if err != nil {
			return err
		}
		args := assembleArgs(ExecOptions{
			SettingsPath: p,
			UserArgs:     userArgs,
		})
		printCommand(os.Stdout, resolve, prefix, args)
		return nil
	})
}

// cmdAdd 交互式生成 profile JSON:问 base_url / token / model,写入。
// token 留空时从环境变量读(若有),否则写空字符串(由 claude 自己处理)。
func cmdAdd(name string) int {
	return runOrExit(func() error {
		if err := validateProfileName(name); err != nil {
			return err
		}
		// 已有则确认
		if _, err := os.Stat(profilePath(name)); err == nil {
			fmt.Fprintf(os.Stderr, "ccc: profile %q already exists; overwrite? [y/N]: ", name)
			var ans string
			_, _ = fmt.Scanln(&ans)
			if !strings.EqualFold(ans, "y") {
				return fmt.Errorf("ccc: aborted")
			}
		}

		fmt.Printf("Adding profile %q\n", name)
		fmt.Print("  base_url [https://api.minimaxi.com/anthropic]: ")
		var baseURL string
		_, _ = fmt.Scanln(&baseURL)
		if baseURL == "" {
			baseURL = "https://api.minimaxi.com/anthropic"
		}
		fmt.Print("  token (留空从环境变量读): ")
		var token string
		_, _ = fmt.Scanln(&token)
		if token == "" {
			if t := os.Getenv("ANTHROPIC_AUTH_TOKEN"); t != "" {
				token = t
				fmt.Println("  (using $ANTHROPIC_AUTH_TOKEN)")
			} else if t := os.Getenv("ANTHROPIC_API_KEY"); t != "" {
				token = t
				fmt.Println("  (using $ANTHROPIC_API_KEY)")
			}
		}
		fmt.Print("  model [MiniMax-M3]: ")
		var model string
		_, _ = fmt.Scanln(&model)
		if model == "" {
			model = "MiniMax-M3"
		}

		body := buildProfileJSON(baseURL, token, model)
		if err := writeProfile(name, body); err != nil {
			return err
		}
		fmt.Printf("✓ written %s\n", profilePath(name))
		return nil
	})
}

// buildProfileJSON 渲染一个 settings 文件 JSON;token 留空时字段省略。
// 字段与 settings_minimax_m3.template.json 对齐(子集)。
func buildProfileJSON(baseURL, token, model string) []byte {
	const tpl = `{
  "env": {
%s
    "ANTHROPIC_BASE_URL": %q,
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": %q,
    "ANTHROPIC_DEFAULT_OPUS_MODEL": %q,
    "ANTHROPIC_DEFAULT_SONNET_MODEL": %q,
    "ANTHROPIC_MODEL": %q,
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
    "allow": ["Bash", "Edit", "MultiEdit", "Write", "Read", "Glob", "Grep", "LS", "WebFetch", "WebSearch", "Rm"]
  },
  "skipDangerousModePermissionPrompt": true
}
`
	tokenLine := ""
	if token != "" {
		tokenLine = fmt.Sprintf("    \"ANTHROPIC_AUTH_TOKEN\": %q,\n", token)
	}
	out := fmt.Sprintf(tpl, tokenLine, baseURL, model, model, model, model)
	return []byte(out)
}

func cmdRm(name string) int {
	return runOrExit(func() error {
		if err := removeProfile(name); err != nil {
			return err
		}
		fmt.Printf("✓ removed %s\n", profilePath(name))
		return nil
	})
}

// ----- profile / profile:continue 模式 -----

// cmdProfile 解析形如:
//   ccc <profile> [args...]                    → 交互透传
//   ccc <profile>:continue [args...]            → 续接上一会话(headless)
//   ccc <profile> -p "task" [args...]           → headless 一次性
func cmdProfile(args []string) int {
	head := args[0]
	continueMode := false
	profile := head
	if i := strings.LastIndex(head, ":"); i > 0 && strings.HasSuffix(head, ":continue") {
		profile = head[:i]
		continueMode = true
	}

	p, err := resolveProfile(profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, ErrProfileNotFound) {
			return 3
		}
		return 1
	}

	rest := args[1:]
	// 检测 -p / --prompt 以决定 headless
	headless, promptIdx, promptErr := detectPrompt(rest)
	if promptErr != nil {
		fmt.Fprintln(os.Stderr, promptErr)
		return 2
	}
	// continue 模式强制 headless
	if continueMode {
		headless = true
		if !hasPromptFlag(rest) {
			// continue 可不带 -p(裸启动也走 headless)
			promptIdx = -1
		}
	}

	if continueMode && !headless {
		// 防止 headless=false 漏掉 continue 标记
		headless = true
	}

	if !headless {
		// 交互模式:整组透传给 claude
		return runOrExit(func() error {
			resolve, prefix, err := findClaude()
			if err != nil {
				return err
			}
			all := assembleArgs(ExecOptions{SettingsPath: p, UserArgs: rest})
			return spawnInteractive(resolve, prefix, all)
		})
	}

	// headless 模式
	return runOrExit(func() error {
		resolve, prefix, err := findClaude()
		if err != nil {
			return err
		}
		// 续接 → 取 session_id
		var sessionID string
		if continueMode {
			sid, err := loadSession(profile)
			if err != nil {
				return fmt.Errorf("load sessions: %w", err)
			}
			if sid == "" {
				return fmt.Errorf("ccc: no previous session for profile %q; "+
					"run `ccc %s -p \"task\"` first", profile, profile)
			}
			sessionID = sid
		}
		// 重组 UserArgs:把 -p 提到最前(顺序与 headless-executor.ts 对齐),剩余原样追加
		userArgs := arrangeHeadlessArgs(rest, promptIdx)

		args := assembleArgs(ExecOptions{
			SettingsPath: p,
			Headless:     true,
			Resume:       continueMode,
			SessionID:    sessionID,
			UserArgs:     userArgs,
		})
		res, err := spawnHeadless(resolve, prefix, args)
		if err != nil {
			return err
		}
		// 把纯文本回复打到 stdout
		if res.Text != "" {
			io.WriteString(os.Stdout, res.Text)
		}
		// 落盘 session_id(即使非 continue 模式也存,方便下次 resume)
		if res.SessionID != "" {
			if err := saveSession(profile, res.SessionID); err != nil {
				fmt.Fprintln(os.Stderr, "ccc: warn: save session:", err)
			}
		}
		if res.ExitCode != 0 {
			// 透传非零退出码
			os.Exit(res.ExitCode)
		}
		return nil
	})
}

// detectPrompt 在 args 中查找 -p 或 --prompt;返回是否 headless + 该 flag 的索引(值在 idx+1)。
// 不允许 -p 在最后(无值)。
func detectPrompt(args []string) (headless bool, idx int, err error) {
	for i, a := range args {
		if a == "-p" || a == "--prompt" {
			if i+1 >= len(args) {
				return false, -1, fmt.Errorf("ccc: flag %q requires a value", a)
			}
			return true, i, nil
		}
		if strings.HasPrefix(a, "-p=") || strings.HasPrefix(a, "--prompt=") {
			return true, i, nil
		}
	}
	return false, -1, nil
}

func hasPromptFlag(args []string) bool {
	for _, a := range args {
		if a == "-p" || a == "--prompt" ||
			strings.HasPrefix(a, "-p=") || strings.HasPrefix(a, "--prompt=") {
			return true
		}
	}
	return false
}

// arrangeHeadlessArgs 把 -p/-p= 提到最前,其余原序追加。
// 形式化为"先 [-p 或 -p=xxx],再剩余原序";与 headless-executor.ts
// 的 ['-p', processedPrompt, '--settings', ...] 一致。
func arrangeHeadlessArgs(args []string, promptIdx int) []string {
	if promptIdx < 0 {
		return append([]string(nil), args...)
	}
	out := make([]string, 0, len(args))
	promptArg := args[promptIdx]
	out = append(out, promptArg)
	// -p <v>:值在 idx+1
	if promptArg == "-p" || promptArg == "--prompt" {
		out = append(out, args[promptIdx+1])
	}
	for i, a := range args {
		if i == promptIdx || i == promptIdx+1 {
			continue
		}
		out = append(out, a)
	}
	return out
}

// printCommand 拼成可粘贴的命令行。prefix 与 args 用空格隔开,统一加双引号。
func printCommand(w io.Writer, resolve string, prefix, args []string) {
	parts := make([]string, 0, 1+len(prefix)+len(args))
	parts = append(parts, quoteArg(resolve))
	parts = append(parts, quoteArgs(prefix)...)
	parts = append(parts, quoteArgs(args)...)
	fmt.Fprintln(w, strings.Join(parts, " "))
}

func quoteArgs(ss []string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = quoteArg(s)
	}
	return out
}

func quoteArg(s string) string {
	if s == "" {
		return `""`
	}
	// 含空格 / 引号 / 路径分隔符时包双引号;内部双引号转义
	if !strings.ContainsAny(s, ` \t"'`) {
		return s
	}
	return `"` + strings.ReplaceAll(s, `"`, `\"`) + `"`
}

// ----- 使用说明 -----

func printUsage(w io.Writer) {
	fmt.Fprintln(w, `ccc — Go single-exe replacement for ccs.

Usage:
  ccc <profile> [args...]                spawn interactive claude with profile
  ccc <profile> -p "task" [args...]       headless single-turn (-p required)
  ccc <profile>:continue [args...]        headless, resuming last session
  ccc list                                list installed profiles
  ccc add <name>                          interactive profile generator
  ccc rm <name>                           remove profile
  ccc which <profile> [args...]           print the spawn command line
  ccc version                             print version
  ccc help                                this message

Profiles live in: ~/.ccc/profiles/<name>.json
Sessions live in: ~/.ccc/sessions.json`)
}
