# ccc (Go 单 exe) 开工清单

> 目标：替代现有 ccs 的最小核心功能 —— `ccc <profile>` / `ccc <profile> -p "任务"`。
> 技术栈：Go（单 exe，~5–8MB，无运行时依赖）。**不做**：account 隔离、cliproxy、MCP/hook 注入、dashboard。
> 核心设计：profile json 文件本身就是 `--settings` 参数，**零转换原样透传**。

## Phase 0 — 项目脚手架

- [ ] `go mod init ccc`，目录结构（文件职责与依赖方向见 `../go架构规范.md` 第 1 节）：
  ```
  ccc/
  ├── main.go        # 唯一入口：错误统一处理退出
  ├── cli.go         # 命令分发
  ├── exec.go        # 唯一允许 spawn 的文件
  ├── profiles.go    # profile 增删查（纯文件操作）
  └── paths.go       # 路径常量集中
  ```
- [ ] `build.ps1`：`go build -ldflags "-s -w" -o dst/ccc.exe ./src`(单 exe 约 2.5 MB,不安装、不改 PATH)
- [ ] 验证：`ccc version` 输出版本号；exe 体积 <10MB

## Phase 1 — 核心透传（MVP，最先交付）

- [ ] profile 解析：`~/.ccc/profiles/<name>.json` 存在即合法（文件内容 = settings 文件，不做字段校验）
- [ ] `ccc <profile> [任意参数...]` → `claude --settings <profilePath> [参数...]`，`stdio` 全部 inherit
- [ ] Windows shim 处理（**抄 `reference/shell-executor.ts` 的思路**）：claude 在 Windows 上是 `.cmd`/`.ps1`，需要 `cmd /c claude ...` 包裹或 PowerShell `-File` 分支
- [ ] 环境清理：spawn 前剥掉父进程 `ANTHROPIC_*` 变量（防嵌套污染，见 `reference/shell-executor.ts` 的 strip 逻辑）
- [ ] 退出码透传
- [ ] 验证：`ccc minimax_m3` 进入交互会话，`/model` 显示 MiniMax-M3；全局 CLAUDE.md、skills、hooks 均生效（用调研结论核对：`--settings` 不隔离这些）

## Phase 2 — profile 管理

- [ ] `ccc list` — 列出 profiles 目录下所有 json
- [ ] `ccc add <name>` — 交互提问 base_url / token / model，生成 json（模板见 `reference/settings_minimax_m3.template.json`）；token 支持留空从环境变量读
- [ ] `ccc rm <name>`
- [ ] `ccc which <profile>` — 打印将执行的完整命令行（调试利器）
- [ ] 验证：add 一个新 profile → which 输出正确 → list/rm 闭环

## Phase 3 — `-p` 与会话恢复

- [ ] `ccc <profile> -p "任务"` → 同样透传 `claude -p "任务" --settings ...`（先不包装输出，原生输出即可读）
- [ ] `ccc <profile>:continue` — `--output-format stream-json` 抓 `session_id` 存 `~/.ccc/sessions.json`，下次 `--resume <id>`（参考 `reference/headless-executor.ts` 的 spawn 参数组装）
- [ ] 验证：`-p` 跑一个简单任务；continue 恢复同一上下文（问它上次结果能答出）

## Phase 4 — 收尾

- [ ] readme.md（人用优先：安装、三个命令、profile json 字段说明）
- [ ] 旧数据迁移：把 `~/.ccs/settings_minimax_m3.json` 等 symlink 直接复制/重指向到 `~/.ccc/profiles/`
- [ ] 全局 CLAUDE.md 中的 `ccs <profile> -p` 示例更新为 `ccc`（如果正式切换）

## 从旧项目复制的参考资料

见 `ccc-reference`目录，内含：

| 文件 | 抄什么 |
|---|---|
| `shell-executor.ts` | `execClaude` 的三分支 spawn（直接 exe / .ps1 / .cmd）+ `stripAnthropicEnv` |
| `settings-flow.ts` | `--settings` 参数组装方式（414-433 行）、用户参数原样透传原则 |
| `headless-executor.ts` | `-p` spawn 的完整命令行（stream-json / permission-mode / resume） |
| `delegation-handler.ts` | flag 解析与未知 flag 透传的分流模式 |
| `ccs.ts` | 入口分流：`profile` / `profile:continue` / 子命令 的判断顺序 |
| `settings_minimax_m3.template.json` | 真实 profile 模板（token 已脱敏），Phase 2 add 命令照此生成 |

原始ccs目录：`E:/__work/__github/ccs`

## 验收标准：
ccs验收标准.md

## 需要数据到这里找：
`C:/Users/yg/.ccs`
`E:/__work/__frequently/personal_data/apikey/decrypt/cc_switcher`
