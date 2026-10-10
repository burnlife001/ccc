package main

// cli.go —— 命令分发:version/list/which/<profile>(含 -c/-p 透传)。
// 只组合 profiles/exec/paths 暴露的函数,不引入新逻辑。

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// profileRef 是 resolveProfile 的返回:
//   - Path:  给 --settings 用的绝对路径(本地 = 持久文件;kdbx = 临时文件)
//   - Source: "local" / "kdbx"
//   - TempPath: 仅 kdbx 源非空,用于 defer os.Remove
type profileRef struct {
	Path     string
	Source   string // "local" | "kdbx"
	TempPath string // kdbx 源时 = Path;本地源时为空
}

// cleanup 若 kdbx 源则删临时文件;本地源 no-op。
// 错误吞掉(claude 退出后 temp 本来就会被 CleanupTempFiles 清)。
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
	}

	// 默认:<profile>(含 -c/-p 透传)
	return cmdProfile(args)
}

// runOrExit 是错误处理统一出口:错误则打印并返回非零 code。
// 错误消息按惯例:用户输入类错误在源站已带 "ccc: " 前缀,系统类错误不带;
// 这里原样打印,不再二次前缀,避免 "ccc: ccc: ..."。
//
// exitCodeErr: 透传 exit code(用于 headless 模式 spawn 透传 claude 退出码,
// 避免直接 os.Exit 把外层 defer 跳过导致临时文件泄漏)。
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

// listEntry 描述一条 profile 来源 + 名字 + 描述,供 cmdList 输出。
type listEntry struct {
	Name   string
	Source string // "local" / "kdbx"
	Desc   string // local = 绝对路径;kdbx = "kdbx:cc/<entry> (notes.profiles.<key>)"
}

// listEntries 双源合并,同名时 local 优先标 [override]。
// kdbx 加载失败时返回 warn 字符串(由 cmdList 在末尾打到 stderr);
// 出错不影响本地 profile 的展示。
func listEntries() (entries []listEntry, kdbxWarn string) {
	localSet := map[string]string{} // name → 绝对路径
	for _, n := range mustListLocal() {
		localSet[n] = profilePath(n)
	}
	kdbxSet := map[string]ProfileSource{} // name → 所在 entry
	if _, sources, err := ListProfilesRich(); err != nil {
		kdbxWarn = err.Error()
	} else {
		for _, s := range sources {
			kdbxSet[s.Name] = s
		}
	}

	seen := map[string]bool{}
	var out []listEntry

	// 先本地(含 override)
	for n, path := range localSet {
		seen[n] = true
		e := listEntry{Name: n, Source: "local", Desc: path}
		if _, ok := kdbxSet[n]; ok {
			e.Source = "override"
			e.Desc = path + " (shadows kdbx:" + kdbxGroup() + "/" + kdbxSet[n].EntryTitle + ")"
		}
		out = append(out, e)
	}
	// 再 kdbx 中独有
	for name, src := range kdbxSet {
		if seen[name] {
			continue
		}
		out = append(out, listEntry{
			Name:   name,
			Source: "kdbx",
			Desc:   "kdbx:" + kdbxGroup() + "/" + src.EntryTitle + " (notes.profiles." + name + ")",
		})
	}
	// 字母序
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1].Name > out[j].Name; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out, kdbxWarn
}

func cmdList() int {
	return runOrExit(func() error {
		entries, kdbxWarn := listEntries()
		if len(entries) == 0 {
			fmt.Fprintln(os.Stderr, "(no profiles installed)")
			fmt.Fprintln(os.Stderr, "  bootstrap: copy settings_*.json → "+profilesDir()+`\`)
			fmt.Fprintln(os.Stderr, "  or:        drop a settings_*.json into "+profilesDir()+`  /  edit kdbx group `+kdbxGroup())
			if kdbxWarn != "" {
				fmt.Fprintln(os.Stderr, "  kdbx warn: "+kdbxWarn)
			}
			return nil
		}
		for _, e := range entries {
			fmt.Printf("[%s] %s\n", e.Source, e.Name)
		}
		if kdbxWarn != "" {
			fmt.Fprintln(os.Stderr, "  kdbx warn: "+kdbxWarn)
		}
		return nil
	})
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
		if ref.Source == "kdbx" {
			fmt.Fprint(os.Stdout, "[kdbx] ")
		}
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
	// kdbx 源: claude 退出后清临时文件(defer 在 main.go 里更合适;
	// 这里 runOrExit 包了一层 lambda,所以 defer 写在 lambda 顶部更近)
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
	fmt.Fprintln(w, "ccc — Go single-exe replacement for ccs.")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "  ccc <profile> [args...]                spawn interactive claude with profile")
	fmt.Fprintln(w, "  ccc <profile> -p \"task\" [args...]       headless single-turn (-p required)")
	fmt.Fprintln(w, "  ccc <profile> -c -p \"task\" [args...]    headless, claude -c 续接上次会话")
	fmt.Fprintln(w, "  ccc list                                list installed profiles")
	fmt.Fprintln(w, "  ccc which <profile> [args...]           print the spawn command line")
	fmt.Fprintln(w, "  ccc version                             print version")
	fmt.Fprintln(w, "  ccc help                                this message")
	fmt.Fprintln(w, "")
	fmt.Fprintln(w, "Profiles live in: "+profilesDir()+`\<name>.json  (与 ccc.exe 同级的 .ccc/)`)
}
