# ccc Go 架构规范

> 适用范围：ccc 全仓库。原则：约束架构方向，不管代码风格（风格交给 `gofmt` + `go vet`，不额外定）。
> 规范优先级低于"简洁"：任何一条若让代码变复杂，放弃该条。

## 1. 包结构（固定六文件、四层，不增不减）

```
main.go        # 唯一入口：解析 os.Args → 调用内部包 → 统一处理错误退出
cli.go         # 命令分发：version/list/which/<profile>(含 -c/-p 透传)
exec.go        # 唯一允许 exec.Command 的文件：claude 查找、spawn、stdio 接线
profiles.go    # profile 文件的读写 + kdbx 合成后的临时 settings 文件管理
paths.go       # 唯一允许计算路径的文件：<exe>/.ccc 等常量集中于此
kdbx.go        # 唯一允许 import gokeepasslib + syscall.advapi32 的文件：kdbx 直读 + settings 合成
```

- 依赖方向严格单向：`main → cli → {exec, profiles, kdbx} → paths`。下层**禁止** import 上层；`exec`、`profiles`、`kdbx` 互不 import。
- 全部包属于 `package main`（单 exe 不拆 module），用文件分职责而非目录。

## 2. 错误处理

- error 逐层向上返回，**只在 main 打印并 `os.Exit(1)`**；业务代码不调 `os.Exit`。
- 禁止裸 `panic`；`main` 里用 `defer func(){ recover() }()` 兜底，panic 时打印堆栈并 exit 2。
- 包装用 `fmt.Errorf("spawn claude: %w", err)`，每层只加本层上下文。
- 用户输入错误（profile 不存在、json 非法）走独立错误类型或约定前缀 `ccc: `，main 据此打印用法提示。

## 3. 进程边界

- `exec.go` 是唯一进程边界：外部命令查找（`exec.LookPath` + Windows `.cmd/.ps1` 分支）、`ANTHROPIC_*` 环境剥离、stdio inherit，全部收在这一个文件。
- 其他文件需要"跑命令"时，只能调 `exec.go` 暴露的函数，不自己 import `os/exec`。

## 3.5 凭证边界

- `kdbx.go` 是唯一允许 import `syscall.advapi32`（Windows Credential Manager）和 `gokeepasslib` 的文件：主密码读取 + kdbx 解密 + 内存合成 settings JSON，全部收在这一个文件。
- 其他文件需要"取 kdbx 数据"时，只能调 `kdbx.go` 暴露的 `LoadProfile` / `ListProfiles`，不自己读 credential 或解密 kdbx。
- 主密码在使用后立即清引用；不写入 log / 不进 error 链。

## 4. 数据与路径

- 路径只从 `paths.go` 的函数拿，**禁止**业务代码里出现 `filepath.Join(home, ...)` 字面量。
- profile 文件内容不做字段校验（零转换透传是核心设计），只校验"文件存在 + 是合法 JSON"。
- 不写注册表、不存绝对路径到配置文件；一切从 `<exe所在目录>/.ccc` 运行时推导（`os.Executable()`；`CCC_HOME` 环境变量仅作测试隔离钩子）。

## 5. 抽象纪律

- **不写 interface，除非存在第二个实现**；mock 只许在 `_test.go` 里定义。
- 无 DI 容器、无配置框架、无日志库——输出用 `fmt.Fprintf(os.Stderr, ...)`。
- 每个导出（大写）函数必须被 `main` 所在包之外调用，否则改小写。

## 6. 测试

- `paths.go` / `profiles.go` 纯逻辑：表驱动测试，覆盖率不作指标要求。
- `exec.go`：不测真实 spawn，只测参数组装（把组装抽成纯函数再测）。
- 验收以 `ccs验收标准.md` 为准，单元测试是辅助。

## 7. 禁止清单

- 禁止引入第三方依赖（标准库够用；若破例须写明理由在 commit message）。
  - **破例**：`github.com/tobischo/gokeepasslib/v3`（KDBX 3.1/4.0/4.1 解密）。
  - **理由**：自研 KDBX crypto（AES-KDF / Argon2 / ChaCha20 组合 + Salsa20 protected stream）在 Go stdlib 范围内不可靠；pykeepass 体积过大不能带进单 exe。
  - 破例仅限这一个 module；任何新增依赖需要重新审批并写入 commit message。
- 禁止 goroutine + channel（CLI 是顺序程序；超时将来用 `exec.CommandContext`）。
- 禁止 `init()` 函数和包级可变变量（`var` 只允许 `const` 或不可变值）。
