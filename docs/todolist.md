# ccc (Go 单 exe + 内嵌 kdbx 源) 开工清单

> 目标：替代现有 ccs，并把 `cc_switcher_kdbx.ps1 + compose_settings.py` 的 kdbx 读取能力吸收进单 exe。
> 技术栈：Go 单 exe。**唯一第三方依赖** `github.com/tobischo/gokeepasslib`（2026-09 活跃，支持 KDBX 3.1/4.0/4.1）——为读取 kdbx 必须破例 `go架构规范.md` §7，破例理由写入对应 commit message。
> 核心设计（方案 B：kdbx 直读，零落盘）：
> - profile 的唯一真相是 **kdbx**（`.kdbx` 文件，KeePass 加密）
> - ccc 启动时**直接从 kdbx 解密并合成 settings JSON**，写入**内存临时文件**（0600），claude 退出后立即删除
> - **明文 token 永不落盘**；`dst/.ccc/profiles/` 只存**本地手动放置的 profile**（fallback/调试用）
> - **不写 `~/.claude/settings.json`**（旧 ps1 的覆盖行为废弃，验收标准的"明确不做"已禁此）

## 依赖与凭证

- `go get github.com/tobischo/gokeepasslib`（commit message 注明：唯一破例依赖，理由 = Go 自研 kdbx crypto 不可靠，pykeepass 不能带进 exe）
- kdbx 主密码：从 Windows Credential Manager 读取 `target=apikey_kdbx_master`（沿用现有约定；用 `syscall` + `advapi32.CredReadW` 直读，不再引外部 Go cred 库，避免第二破例）
- kdbx 路径：默认 `E:\__work\__frequently\personal_data\kdbx_cpq\apikey.kdbx`，group = `cc`，可被 `CCC_KDBX_PATH` / `CCC_KDBX_GROUP` 环境变量覆盖（仅测试用）

## Phase 0 — 项目脚手架 + kdbx spike

- [ ] `go mod init ccc`，`go get github.com/tobischo/gokeepasslib`
- [ ] **Spike**：5 行 main 验证 gokeepasslib 能打开当前 kdbx + 列出 `cc` 组 entry 名。失败立即降级方案 3（ps1 产出 profiles，ccc 只读本地 json）。
- [ ] 目录结构（在 `go架构规范.md` 五文件基础上**新增 kdbx.go**，共六文件，规范文档同步修订）：
  ```
  ccc/
  ├── main.go        # 唯一入口：错误统一处理退出
  ├── cli.go         # 命令分发：version/list/which/<profile>/<profile>:continue
  ├── exec.go        # 唯一允许 os/exec 的文件
  ├── profiles.go    # 本地 profile 文件 CRUD + 内存临时文件管理
  ├── paths.go       # 路径常量集中
  └── kdbx.go        # 唯一允许 import gokeepasslib + advapi32 的文件：kdbx → 内存 settings
  ```
- [ ] 依赖方向更新为 `main → cli → {exec, profiles, kdbx} → paths`；`exec`/`profiles`/`kdbx` 互不 import
- [ ] `build.ps1`：`go build -ldflags "-s -w" -o dst/ccc.exe ./src`（含 gokeepasslib，目标体积 < 6 MB）
- [ ] 验证：`ccc version` 输出版本号 + exit 0；exe 体积 < 6 MB

## Phase 1 — 核心透传（MVP，最先交付）

- [ ] **profile 查找双源**：先查本地 `<exe同级>/.ccc/profiles/<name>.json`，找不到再查 kdbx
- [ ] **kdbx 直读**：`kdbx.go :: LoadProfile(name string) ([]byte, error)` — 读 master → 开 kdbx → 遍历 group 找匹配 profile 名 → 内存合成 settings JSON → 返回字节
- [ ] **临时文件**：`profiles.go :: WriteTempSettings(data []byte) (string, error)` — `os.CreateTemp("", "ccc-settings-*.json")` + `0600` + 返回路径
- [ ] `ccc <profile> [任意参数...]` → `claude --settings <临时文件路径> [参数...]`，stdio inherit
- [ ] Windows shim 三分支（抄 `reference/shell-executor.ts`）：直 exe / `.cmd` 走 `cmd /c` / `.ps1` 走 `powershell -File`
- [ ] spawn 前剥掉父进程 `ANTHROPIC_*`
- [ ] 退出码透传；claude 退出后 `os.Remove` 临时文件（defer 在 main 里）
- [ ] 验证：`ccc minimax_m3` 进交互，`/status` 显示 base_url 来自 kdbx 合成的 settings

## Phase 2 — profile 管理

- [ ] `ccc list` — 双源合并：
  - 本地：扫 `<exe同级>/.ccc/profiles/*.json`
  - kdbx：扫 group `cc` 的每条 entry，展开 `notes.profiles{}` 的 key
  - 输出格式：`[local] name → path` / `[kdbx] name → kdbx:cc/<entry>`（同名时 local 优先，标 `[override]`）
- [ ] `ccc which <profile>` — 打印将执行的命令；kdbx 源时打印临时文件路径 + `[kdbx]` 标记
- [ ] 验证：which → list 闭环；kdbx 源的 profile 能 list 到

## Phase 3 — `-p` 与会话恢复

- [ ] `ccc <profile> -p "任务"` — 同 Phase 1 链路，但走 stream-json 输出路径
- [ ] `ccc <profile>:continue` — `--output-format stream-json` 抓 `session_id` 写 `<exe同级>/.ccc/sessions.json`，下次 `--resume <id>`
- [ ] **kdbx 源也支持 continue**：session_id 存本地 `sessions.json`（不含 token，安全）
- [ ] 验证：6.1 / 6.2 / 6.3 三行全过（kdbx 源和本地源各跑一次）

## Phase 4 — kdbx 合成引擎（1:1 移植 compose_settings.py）

> 全部在 `kdbx.go`，对外暴露 `LoadProfile(name string) ([]byte, error)` 和 `ListProfiles() ([]string, error)`。

- [ ] `kdbx.go :: readCredAdvapi32(target string) (string, error)` — `advapi32.CredReadW` P/Invoke（参考 ps1 行 21–70 结构，照抄语义：CRED_TYPE_GENERIC=1，blob 是 UTF-16）
- [ ] `kdbx.go :: openKdbx(path, masterPassword string)` — gokeepasslib 打开 + 解锁，group `cc` 不存在时报错
- [ ] `kdbx.go :: findGroup(root, name)` — 递归找组（对应 ps1 的 `find_group`）
- [ ] `kdbx.go :: parseProviderRecord(entry) → (profiles map[string]profileSpec, baseURL string, extraEnv map, ok bool)` — 解析 notes JSON（结构 = `compose_settings.py` 的 `notes` schema：`base_url` + `profiles{}` + 可选 `extra_env`）
- [ ] `kdbx.go :: composeSettings(notes, token, profileName) → map` — 1:1 移植 `compose_settings.py` 的 `compose()`：骨架 `SKELETON_ENV` + `extra_env` + `ANTHROPIC_BASE_URL` + `ANTHROPIC_AUTH_TOKEN` + 三档模型覆盖 + `ANTHROPIC_MODEL` + `permissions{defaultMode:bypassPermissions, allow:ALLOW}` + `skipDangerousModePermissionPrompt:true`
  - **去掉 `current_Model_Provider` 顶层字段**（ccc 模式下 `--settings` 已表明身份，此字段无消费者）
- [ ] `kdbx.go :: serializeJSON(obj) → []byte` — 1:1 移植 `serialize()`：2 空格缩进 + `allow` 数组单行紧凑 + 末尾 `\n`（**字节级复刻**，保证与 Python 输出一致）
- [ ] `kdbx.go :: LoadProfile(name string) ([]byte, error)` — 读 master → 开 kdbx → 遍历 group 每条 ccp-v1 记录 → 找 `profiles[name]` → 合成 → 序列化 → 返回
  - 主密码用后立即置空字符串引用（Go 无法强制清零，至少尽早释放）
  - 找不到 profile 名 → 报错 `profile not found in kdbx: <name>`
- [ ] `kdbx.go :: ListProfiles() ([]string, error)` — 遍历 group，收集所有 `profiles{}` 的 key
- [ ] 验证：
  - [ ] 4.1 `LoadProfile("minimax_m3")` 返回的字节与 `compose_settings.py` 对同一条 entry 的输出**逐字节 diff 一致**（除去掉 `current_Model_Provider`）
  - [ ] 4.2 `ListProfiles()` 返回所有 kdbx 里的 profile 名
  - [ ] 4.3 `ccc <kdbx profile> -p "回复OK"` 跑通（证明 token 注入 + base_url 正确）
  - [ ] 4.4 kdbx 主密码错误/不存在 → 明确报错，不 panic，不写任何文件
  - [ ] 4.5 kdbx 路径不存在 → 明确报错指向 `CCC_KDBX_PATH`

## Phase 5 — 收尾

- [ ] readme.md（人用优先：安装、六个命令、profile json 字段说明、kdbx 直读机制、本地 fallback 行为）
- [ ] 旧数据迁移：`~/.ccs/settings_minimax_m3.json` 复制到 `dst/.ccc/profiles/minimax_m3.json`（本地 fallback 用）
- [ ] 老 `cc_switcher_kdbx.ps1` 在 readme 标记 **deprecated**，指引到 ccc（不删，留作 kdbx 流水线对照实现）
- [ ] `go架构规范.md` §1 增列 `kdbx.go`，§7 的"禁第三方依赖"补一条破例说明（gokeepasslib + 理由）
- [ ] `ccc验收标准.md` 补 P1 一节：kdbx 直读相关 4.1–4.5 五行

## 从旧项目复制的参考资料

| 文件 | 抄什么 |
|---|---|
| `reference/shell-executor.ts` | `execClaude` 三分支 spawn + `stripAnthropicEnv` |
| `reference/settings-flow.ts` | `--settings` 参数组装 + 用户参数原样透传 |
| `reference/headless-executor.ts` | `-p` 命令行 / stream-json / resume |
| `reference/delegation-handler.ts` | flag 解析与未知 flag 透传分流 |
| `reference/ccs.ts` | 入口分流：`profile` / `profile:continue` / 子命令 |
| `reference/settings_minimax_m3.template.json` | kdbx `notes.profiles{}` schema 样本 + 本地 fallback 模板 |
| `E:/__work/__frequently/personal_data/apikey/decrypt/cc_switcher/cc_switcher_kdbx.ps1` | `advapi32.CredReadW` P/Invoke 结构 + pykeepass 调法 + 菜单行为参考 |
| `E:/__work/__frequently/personal_data/apikey/decrypt/cc_switcher/src/compose_settings.py` | **Phase 4 的 1:1 移植源**：`compose()` / `serialize()` / `SKELETON_ENV` / `ALLOW` / 字节级序列化 |

原始 ccs：`E:/__work/__github/ccs`

## 验收标准

`ccc验收标准.md`（Phase 4 完成后回写 kdbx 直读相关 4.1–4.5）

## 需要数据到这里找

- 旧 ccs 数据：`C:/Users/yg/.ccs`
- kdbx 工具现状：`E:/__work/__frequently/personal_data/apikey/decrypt/cc_switcher`
- kdbx 文件：`E:/__work/__frequently/personal_data/kdbx_cpq/apikey.kdbx`
- 主密码：Windows Credential Manager `target=apikey_kdbx_master`

## 关键风险与对策

| 风险 | 对策 |
|---|---|
| gokeepasslib 不支持当前 kdbx 的具体变体（AES-KDF / Argon2 / ChaCha20 组合） | Phase 0 立刻 spike：5 行 main 读 kdbx + 列组名。失败立即降级方案 3（ps1 产出 profiles，ccc 只读本地 json） |
| `advapi32.CredReadW` 的 UTF-16 blob 解码边界 | 对照 ps1 行 60–66 的字节复制 + Unicode.GetString 行为，Go 侧用 `utf16.Decode` |
| `serialize()` 的字节级复刻遗漏（空格、缩进、换行） | Phase 4.1 验收强制要求与现有 Python 输出**逐字节 diff**（去掉 `current_Model_Provider` 后）一致 |
| 主密码在 Go 内存驻留 | kdbx 解锁成功后立刻 `master = ""`；不写入 log / 不进 error 链 |
| kdbx 解密延迟（~200–500 ms）对 `-p` 高频调用不友好 | 文档注明；高频场景建议本地 `profiles/` 放一份（fallback 机制天然支持） |
| 临时文件在 claude 崩溃后残留 | `os.CreateTemp` 前缀 `ccc-settings-*`，ccc 启动时扫一遍清理 |
| 离线场景（kdbx 所在盘未挂载） | 本地 `profiles/` 兜底；报错信息明确提示"kdbx 不可达，可用本地 profile 或检查路径" |
