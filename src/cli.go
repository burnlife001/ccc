package main

// cli.go —— 命令分发:version/list/which/<profile>(含 -c/-p 透传)。
// 只组合 profiles / kdbx / exec / paths 暴露的函数,不引入新逻辑。
//
// v2 简化:ccc 只走 kdbx 源,移除所有 local profile source 相关分支。

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// profileRef 是 resolveProfile 的返回:
//   - Path: 给 --settings 用的绝对路径(kdbx = 临时文件)
//   - TempPath: 调用方负责 defer os.Remove
type profileRef struct {
	Path     string
	TempPath string // 始终 == Path(只有 kdbx 源);保留字段以让 cleanup 行为显式
}

// cleanup 删除 kdbx 源的临时 settings 文件。best-effort。
func (r profileRef) cleanup() {
	if r.TempPath != "" {
		_ = os.Remove(r.TempPath)
	}
}

// Dispatch 是 cli 的唯一对外入口。main 把 os.Args[1:] 传进来。
// 返回 exit code:0 成功;非 0 失败。
func Dispatch(args []string) int {
	// 启动时清一次残留(应对上一次 claude 崩溃 / 进程被 kill)
	CleanupTempFiles()

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
	case "cred":
		return cmdCred(args[1:])
	}

	// 默认:<profile>(含 -c/-p 透传)
	return cmdProfile(args)
}

// runOrExit 是错误处理统一出口:错误则打印并返回非零 code。
func runOrExit(fn func() error) int {
	if err := fn(); err != nil {
		var ec *exitCodeErr
		if errors.As(err, &ec) {
			return ec.Code
		}
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, ErrProfileNotFound) {
			return 3
		}
		return 1
	}
	return 0
}

// exitCodeErr 包装非零退出码;runOrExit 据此透传而非打印。
type exitCodeErr struct{ Code int }

func (e *exitCodeErr) Error() string { return fmt.Sprintf("exit %d", e.Code) }

// ----- 子命令实现 -----

// listEntry 描述一条 kdbx profile 来源,供 cmdList 输出。
type listEntry struct {
	Name string
	Desc string // "kdbx:<group>/<entry> (notes.profiles.<key>)"
}

// listEntries 列出 kdbx cc 组所有 profile,字母序。
// kdbx 加载失败时返回 warn 字符串(由 cmdList 在末尾打到 stderr)。
func listEntries() (entries []listEntry, kdbxWarn string) {
	names, sources, err := ListProfilesRich()
	if err != nil {
		kdbxWarn = err.Error()
		return nil, kdbxWarn
	}
	for i, n := range names {
		entries = append(entries, listEntry{
			Name: n,
			Desc: "kdbx:" + kdbxGroup() + "/" + sources[i].EntryTitle + " (notes.profiles." + n + ")",
		})
	}
	return entries, ""
}

func cmdList() int {
	return runOrExit(func() error {
		entries, kdbxWarn := listEntries()
		if len(entries) == 0 {
			fmt.Fprintln(os.Stderr, "(no profiles installed)")
			fmt.Fprintln(os.Stderr, "  check kdbx group "+kdbxGroup()+" at "+kdbxPath())
			if kdbxWarn != "" {
				fmt.Fprintln(os.Stderr, "  kdbx warn: "+kdbxWarn)
			}
			return nil
		}
		for _, e := range entries {
			fmt.Printf("[kdbx] %s\n", e.Name)
		}
		if kdbxWarn != "" {
			fmt.Fprintln(os.Stderr, "  kdbx warn: "+kdbxWarn)
		}
		return nil
	})
}

// ----- cred 子命令 (ccc cred store|check|delete) -----

// cmdCred 分发 cred 子命令。无子命令 / 非法子命令 → 打用法 + 退出码 2。
func cmdCred(subArgs []string) int {
	return runOrExit(func() error {
		if len(subArgs) == 0 {
			printCredUsage(os.Stderr)
			return &exitCodeErr{Code: 2}
		}
		switch subArgs[0] {
		case "store":
			return cmdCredStore()
		case "check":
			return cmdCredCheck()
		case "delete":
			return cmdCredDelete()
		default:
			printCredUsage(os.Stderr)
			return &exitCodeErr{Code: 2}
		}
	})
}

// cmdCredStore 交互式输入密码 → verify → DPAPI 加密落盘。
// 满足 A2(成功路径) / A3(失败不落盘) / A7(非 tty 立即报错)。
func cmdCredStore() error {
	path := kdbxPath()
	if path == "" {
		return errors.New("cred store: CCC_KDBX_PATH not set (add it to " +
			filepath.Join(exeDir(), ".env") + " or the environment)")
	}
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("cred store: kdbx file not found at %s (set CCC_KDBX_PATH)", path)
		}
		return fmt.Errorf("cred store: stat kdbx: %w", err)
	}
	password, err := readPasswordInteractive("Enter kdbx master password: ")
	if err != nil {
		return fmt.Errorf("cred store: %w", err)
	}
	if password == "" {
		return errors.New("cred store: empty password")
	}
	// 验证 + 落盘之间及时清引用
	fmt.Fprintln(os.Stderr, "[store] verifying password against kdbx before writing credential...")
	if err := storeCred(path, password); err != nil {
		// storeCred 内部已 verify → 错误明确为密码错或 DPAPI 错
		return fmt.Errorf("cred store: %w", err)
	}
	defer func() { password = "" }()
	fmt.Fprintln(os.Stderr, "[store] VERIFY_OK: password opens the kdbx")
	fmt.Fprintf(os.Stderr, "[store] STORED: %s (DPAPI, current-user scope)\n", credPathFor(path))
	return nil
}

// cmdCredCheck: 读 cred → DPAPI 解密 → 试开 kdbx。CHECK_OK + exit 0。
func cmdCredCheck() error {
	if err := checkCred(kdbxPath()); err != nil {
		return fmt.Errorf("cred check: %w", err)
	}
	fmt.Println("CHECK_OK")
	return nil
}

// cmdCredDelete: 删 cred 文件。存在 → DELETED;不存在 → NOT_FOUND(均 exit 0)。
func cmdCredDelete() error {
	existed, err := deleteCred(kdbxPath())
	if err != nil {
		return fmt.Errorf("cred delete: %w", err)
	}
	if !existed {
		fmt.Println("NOT_FOUND: nothing to delete")
		return nil
	}
	fmt.Printf("DELETED: %s\n", credPathFor(kdbxPath()))
	return nil
}

func cmdWhich(profile string, userArgs []string) int {
	return runOrExit(func() error {
		ref, err := resolveProfile(profile)
		if err != nil {
			return err
		}
		// which 不留临时文件(claude 不跑);立刻清掉。
		defer ref.cleanup()

		resolve, prefix, err := findClaude()
		if err != nil {
			return err
		}
		args := assembleArgs(ExecOptions{
			SettingsPath: ref.Path,
			UserArgs:     userArgs,
		})
		// kdbx 源加 [kdbx] 标记
		fmt.Fprint(os.Stdout, "[kdbx] ")
		printCommand(os.Stdout, resolve, prefix, args)
		return nil
	})
}

// ----- profile 模式 -----

// cmdProfile 解析形如:
//
//	ccc <profile> [args...]              → 交互透传
//	ccc <profile> -p "task" [args...]     → headless 一次性
//	ccc <profile> -c -p "task" [args...]  → headless,claude 自带 -c 续接上次会话
func cmdProfile(args []string) int {
	profile := args[0]

	ref, err := resolveProfile(profile)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		if errors.Is(err, ErrProfileNotFound) {
			return 3
		}
		return 1
	}
	// kdbx 源: claude 退出后清临时文件
	defer ref.cleanup()

	rest := args[1:]
	// 检测 -p / --prompt 以决定 headless
	headless, promptIdx, promptErr := detectPrompt(rest)
	if promptErr != nil {
		fmt.Fprintln(os.Stderr, promptErr)
		return 2
	}

	if !headless {
		// 交互模式:整组透传给 claude
		return runOrExit(func() error {
			resolve, prefix, err := findClaude()
			if err != nil {
				return err
			}
			all := assembleArgs(ExecOptions{SettingsPath: ref.Path, UserArgs: rest})
			return spawnInteractive(resolve, prefix, all)
		})
	}

	// headless 模式(-c 由 claude 自行处理,这里只透传)
	return runOrExit(func() error {
		resolve, prefix, err := findClaude()
		if err != nil {
			return err
		}
		// 重组 UserArgs:把 -p 提到最前(顺序与 headless-executor.ts 对齐),剩余原样追加
		userArgs := arrangeHeadlessArgs(rest, promptIdx)

		args := assembleArgs(ExecOptions{
			SettingsPath: ref.Path,
			Headless:     true,
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
		if res.ExitCode != 0 {
			// 透传非零退出码 —— 用 error 包装,避免直接 os.Exit 跳过外层 defer (临时文件)
			return &exitCodeErr{Code: res.ExitCode}
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
	fmt.Fprintln(w, "ccc — Go single-exe Claude Code launcher (kdbx source only).")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  ccc <profile> [args...]                spawn interactive claude with profile")
	fmt.Fprintln(w, "  ccc <profile> -p \"task\" [args...]       headless single-turn (-p required)")
	fmt.Fprintln(w, "  ccc <profile> -c -p \"task\" [args...]    headless, claude -c 续接上次会话")
	fmt.Fprintln(w, "  ccc list                                list installed profiles")
	fmt.Fprintln(w, "  ccc which <profile> [args...]           print the spawn command line")
	fmt.Fprintln(w, "  ccc cred <store|check|delete>           manage kdbx master password (DPAPI)")
	fmt.Fprintln(w, "  ccc version                             print version")
	fmt.Fprintln(w, "  ccc help                                this message")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Profiles source: kdbx file at "+kdbxPath()+" (group "+kdbxGroup()+")")
}

// printCredUsage — `ccc cred` 无参数 / 非法子命令时的输出。
func printCredUsage(w io.Writer) {
	fmt.Fprintln(w, "ccc cred <store|check|delete>")
	fmt.Fprintln(w, "  store    prompt for kdbx master password, verify, store DPAPI-encrypted")
	fmt.Fprintln(w, "  check    verify stored credential decrypts and opens the kdbx")
	fmt.Fprintln(w, "  delete   delete stored credential file (.kdbx_master.cred)")
}