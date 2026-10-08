# ccc 整体验收报告 — YG-7

- **验收对象**：`ccc` v0.1.0，Go 单 exe（commit `e70ddec`，`go1.26.3 windows/amd64`，`CGO_ENABLED=0`）
- **验收依据**：`docs/ccs验收标准.md`（P0 1.1-4.2 / P1 5.1-6.5 / P2 7.1-7.3，共 **27 条**）
- **执行环境**：Windows 11 11.0.26300 · git-bash + pwsh 7 · Go 1.26.3 · Claude Code CLI 2.1.294
- **被测二进制**：`D:\Programs\ccc\ccc.exe`（2,603,520 bytes）
- **执行人**：小测（E2E 测试工程师） · **汇报对象**：研总
- **执行日期**：2026-10-08 ~ 2026-10-09

---

## 0. 总结论

| 分级 | 条目数 | 通过 | 失败 | 未测 |
|---|---|---|---|---|
| P0 核心透传 | 14 | 14 | 0 | 0 |
| P1 profile 管理 / `-p` / continue | 10 | 10 | 0 | 0 |
| P2 收尾 | 3 | 3 | 0 | 0 |
| **合计** | **27** | **27** | **0** | **0** |

**判定：27/27 全部通过。P0/P1 全绿，满足「可交付 / 可替换旧 ccs」判定线。**

> ⚠️ 27 条编号矩阵之外另发现 3 项问题：**B-1 为发布阻断项**（readme 提交并推送了真实 MiniMax token），
> 已在 [YG-8](/YG/issues/YG-8) 立项收口，**必须在父任务 YG-1 收口前处理**。详见 §4。

---

## 1. 构建验证

| 操作 | 预期 | 实际 | 判定 |
|---|---|---|---|
| `pwsh -File ./build.ps1` | 编译 0 Error / 0 Warning，安装到 `D:/Programs/ccc/` | `[build] go build -ldflags -s -w -X main.version=0.1.0 -o ccc.exe` → `[install] copied to D:/Programs/ccc/ccc.exe` → `[ok] built and installed version 0.1.0`，exit 0，**无任何 warning/error 输出** | pass |
| `go vet ./...` | 无告警 | 无输出，exit 0 | pass |
| `go test ./...` | 通过 | `ok ccc 0.352s`，exit 0 | pass |

### 架构契约符合性（`go架构规范.md`）

| 约束 | 实测 | 判定 |
|---|---|---|
| 恰好 5 个非测试 `.go` 文件 | `cli.go` `exec.go` `main.go` `paths.go` `profiles.go` = 5 | pass |
| `os/exec` 只允许在 `exec.go` | `grep -ln '"os/exec"' *.go` → 仅 `exec.go` | pass |
| `filepath.Join` 只允许在 `paths.go` | 非测试代码中 0 处（仅 `phase3_test.go` 有 1 处） | pass |
| std-only，无三方依赖 | `go.mod` 无 `require` 块；`go list -deps` 非 std 包仅 `ccc` 自身 | pass |
| 无运行时依赖 | `go version -m` → `CGO_ENABLED=0`、`GOOS=windows` | pass |

---

## 2. 逐条验收明细

### P0 — 核心透传（14 条）

#### 1. 安装与启动

| # | 操作 | 预期 | 实际输出 | 判定 |
|---|---|---|---|---|
| 1.1 | `ccc version` | 语义化版本号，exit 0 | `ccc 0.1.0` / exit 0 | **pass** |
| 1.2 | 查看 `D:\Programs\ccc\ccc.exe` | 单文件，< 10 MB，无伴随 dll/runtime | 目录内仅 `ccc.exe`，`2,603,520 bytes`（2.48 MB） | **pass** |
| 1.3 | `ccc`（无参数） | 打印用法 + exit 非 0，不 panic | 打印完整 Usage，`exit=2`，无 panic 堆栈 | **pass** |
| 1.4 | `ccc nonexistent_profile_zzz` | `profile not found: xxx` + 可用列表，exit 非 0 | `ccc: profile not found: nonexistent_profile_zzz` / `Available profiles: minimax_m3` / exit 3 | **pass** |

#### 2. 交互模式透传

| # | 操作 | 预期 | 实际输出 | 判定 |
|---|---|---|---|---|
| 2.1 | `ccc minimax_m3` | 进入 claude 交互界面，模型 MiniMax-M3 | 管道喂入提问，claude 在会话内作答；stderr `[claude-code:unrecognized_model] {"model":"MiniMax-M3","query_source":"sdk"}`，exit 0 | **pass** |
| 2.2 | 会话中 `/status` | 显示 `ANTHROPIC_BASE_URL=https://api.minimaxi.com/anthropic` | **证据替代**（见下方说明）：子进程 argv 探针 + base_url A/B 路由实验，三重佐证 settings 生效 | **pass**※ |
| 2.3 | 会话中问「读取全局 CLAUDE.md 编码约定」 | 能复述 CLAUDE.md 内容 | 模型回复 `0. **encoding**: UTF-8.` —— 与 `C:\Users\yg\.claude\CLAUDE.md` 第 0 条逐字一致 | **pass** |
| 2.4 | 会话中 `/exit` | shell 拿到 claude 退出码 | 退出码透传，`exit=0` | **pass** |
| 2.5 | `echo 测试 \| ccc minimax_m3` | claude 收到 stdin | 模型回显 `测试`，exit 0 | **pass** |

> ※ **2.2 证据替代说明（必须知悉）**
> 验收标准原文要求以会话内 `/status` 输出取证。本 agent 运行在无控制台的管道环境中，claude 对管道输入直接返回
> `/status isn't available in this environment.`；尝试用 `winpty` 分配真实 pty 亦失败（exit 1）。故改用三条独立证据链证明
> 「`--settings` 生效且其中的 `ANTHROPIC_BASE_URL` 就是实际拨号地址」：
> 1. **子进程 argv 探针**：用 `CCC_CLAUDE` 指向探针脚本，`ccc minimax_m3 --version` 打印出 claude 实际收到的参数为
>    `[--settings] [C:\Users\yg\.ccc\profiles\minimax_m3.json] [--version]`；
> 2. **profile 内容**：`minimax_m3.json` 中 `ANTHROPIC_BASE_URL = https://api.minimaxi.com/anthropic`；
> 3. **base_url A/B 路由实验**（见 5.3）：同一套代码换成探测域名即 `ENOTFOUND` 失败，换回 minimax 即成功 —— 证明
>    settings 文件里的 base_url 正是实际生效的拨号地址。
>
> 三条证据均指向 `--settings` 生效，故判定 pass。**若需字面 `/status` 截图，需在有 TTY 的交互终端由人工复跑一次。**

#### 3. 参数透传

| # | 操作 | 预期 | 实际输出 | 判定 |
|---|---|---|---|---|
| 3.1 | `ccc minimax_m3 --version` | 输出 claude 版本 | `2.1.294 (Claude Code)`，exit 0 | **pass** |
| 3.2 | `ccc which minimax_m3` | `claude --settings <绝对路径>minimax_m3.json`，无 `~`/相对路径 | `"C:\Users\yg\.local\bin\claude.exe" "--settings" "C:\Users\yg\.ccc\profiles\minimax_m3.json"`，exit 0 | **pass** |
| 3.3 | `ccc which minimax_m3 --max-turns 5` | 命令行末尾含 `--max-turns 5` | `"...claude.exe" "--settings" "...minimax_m3.json" "--max-turns" 5`，exit 0 | **pass** |

#### 4. 环境隔离

| # | 操作 | 预期 | 实际输出 | 判定 |
|---|---|---|---|---|
| 4.1 | 污染父进程 `ANTHROPIC_*` 后启动 | 会话 token 来自 profile 而非污染值 | 见下方专项证据 | **pass** |
| 4.2 | git-bash 与 pwsh 各跑一次 2.1 | 两者行为一致 | git-bash → `BASH`，exit 0；pwsh(UTF-8 stdin) → `PWSH`，exit 0 | **pass**※ |

> **4.1 专项证据（决定性）**
> 父进程注入 `ANTHROPIC_BASE_URL=https://ccc-polluted-host.invalid/anthropic`、`ANTHROPIC_AUTH_TOKEN=POLLUTED-VALUE-123`、
> `CLAUDECODE=1`（已验证三项确实传入子进程），再用 `CCC_CLAUDE` 探针截获子进程环境：
> ```
> FAKE-CLAUDE-PROBE argv:
>   [--settings] [C:\Users\yg\.ccc\profiles\minimax_m3.json] [--version]
> FAKE-CLAUDE-PROBE anthropic-env:
>   (无任何 ANTHROPIC_* / CLAUDECODE 变量)
> ```
> 子进程环境中 `ANTHROPIC_*` 与 `CLAUDECODE` **全部消失** ⇒ spawn 前已剥离，token 只可能来自 profile 文件。
> 另：同一污染环境下 `ccc minimax_m3 -p "只回复OK"` 仍返回 `OK` / exit 0。
>
> 未采用 `/status` 取证的另一原因：本机 `~/.claude/settings.json` 也设置了 `ANTHROPIC_BASE_URL`，会覆盖进程级污染值，
> 导致「不剥也会成功」的假阳性 —— 直接探测子进程环境是唯一无歧义的验证方式。

> ※ **4.2 附注**：pwsh 侧必须以 UTF-8 写 stdin。用 PowerShell 原生管道直接喂中文时，claude 会把输入原样回显而不作答
> （已复现，与 ccc 无关）；改用 `ProcessStartInfo.StandardInputEncoding = UTF8` 后正常返回 `PWSH`。
> 两个 shell 行为一致，判定 pass。

### P1 — profile 管理（5 条）

| # | 操作 | 预期 | 实际输出 | 判定 |
|---|---|---|---|---|
| 5.1 | `ccc list` | 列出全部 json，格式 `名称 → 文件路径` | `minimax_m3 → C:\Users\yg\.ccc\profiles\minimax_m3.json`，exit 0 | **pass** |
| 5.2 | `ccc add test_p` | 生成合法 JSON、token 落盘、list 可见 | 交互输入 base_url/token/model → `✓ written C:\Users\yg\.ccc\profiles\test_p.json`；文件 964 bytes，`json.load` 解析通过，`ANTHROPIC_AUTH_TOKEN` 已落盘（长度 125）；`list` 可见 `test_p` | **pass** |
| 5.3 | `ccc test_p` 实际启动 | 路由到 add 时填入的 base_url | A/B 实测：<br>• `test_p`（base_url=minimaxi）→ `-p "只回复OK"` 返回 `OK`，exit 0<br>• `routetest`（同一 `ccc add` 流程，base_url 填 `https://ccc-routetest-probe.invalid/anthropic`）→ `API Error: Can't reach the API server — check your internet or DNS (ENOTFOUND)`，exit 1<br>⇒ base_url 完全由 `ccc add` 填入值决定 | **pass** |
| 5.4 | `ccc rm test_p` | 文件删除、list 不再出现；对不存在 profile 报 not found 不 panic | `✓ removed ...test_p.json` exit 0；`list` 中消失；`ccc rm ghost_profile` → `ccc: profile not found: ghost_profile`，exit 3，无 panic | **pass** |
| 5.5 | 语法错误的 profile | 明确报错并指出文件路径，exit 非 0 | `ccc: invalid JSON in C:\Users\yg\.ccc\profiles\badprofile.json: invalid character 'b' looking for beginning of object key string`，exit 1 | **pass** |

### P1 — `-p` 与 continue（5 条）

| # | 操作 | 预期 | 实际输出 | 判定 |
|---|---|---|---|---|
| 6.1 | `ccc minimax_m3 -p "回复OK两个字"` | 输出 `OK`、exit 0、纯文本非 stream-json | stdout `OK`，exit 0；`grep -cE '"type":"(assistant\|result\|system)"'` → `0`（无 stream-json 信封） | **pass** |
| 6.2 | `ccc minimax_m3 -p "记住数字 8741。只回复收到"` | 回复收到 | `收到`，exit 0 | **pass** |
| 6.3 | `ccc minimax_m3:continue -p "我让你记的数字是？"` | 回答 `8741` | `8741`，exit 0；`~/.ccc/sessions.json` 由 `{minimax_m3: d886dedc-…}` 更新为 `{minimax_m3: b4aa6160-…}`，证明 session_id 捕获与 `--resume` 生效 | **pass** |
| 6.4 | `ccc minimax_m3 -p "数到3就停: 1 2 3 4 5 6 7 8" --max-turns 1` | 参数生效，一轮即停 | 输出 `1 2 3`，exit 0 | **pass** |
| 6.5 | 断网状态下执行 6.1 | 报错可读，exit 非 0 | 以黑洞代理 `HTTPS_PROXY=http://127.0.0.1:1` 模拟断网 → `API Error: Connection refused — a firewall or proxy may be blocking it (ECONNREFUSED)`，exit 1（claude 原生错误原样透传） | **pass** |

### P2 — 收尾（3 条）

| # | 操作 | 预期 | 实际输出 | 判定 |
|---|---|---|---|---|
| 7.1 | 旧 `~/.ccs/settings_minimax_m3.json` 移入 `~/.ccc/profiles/` 后 `ccc minimax_m3 -p "OK?"` | 旧数据零改动直接可用 | 复制前后 md5 均为 `402f3c3833d1b17873b62926d2539f1f`（**字节级一致**）；随后 6.1 / 6.2 / 6.3 全部成功 | **pass** |
| 7.2 | readme.md 内容审查 | 含安装步骤、六个命令、profile json 字段表、完整示例 | 四项齐全（安装见「三步装好」；六个命令 `list/add/rm/which/<profile>/<profile>:continue` 见「六个命令」；字段表 18 行；完整示例含 JSON + 使用流程） | **pass**※ |
| 7.3 | 连续执行 6.1 五十次 | 无句柄/进程泄漏，全部 exit 0 | **50/50 全部 `exit=0 out=OK`**；`TOTAL_FAIL=0`；运行前后 `ccc` 进程数均为 **0** | **pass** |

> ※ **7.2 附注**：四项硬性内容齐备，判定 pass。但审查中发现 readme 与实际实现存在若干**描述不符**，
> 以及一项**安全问题 B-1**。已单列于 §4。

> **7.3 执行记录（两次运行）**
>
> | 运行 | 结果 | 说明 |
> |---|---|---|
> | 第 1 次 | 26 通过 / 24 失败（#27 起 `Not logged in · Please run /login`） | **非 ccc 缺陷**。诊断发现 `~/.ccc/profiles/minimax_m3.json` 在 00:44:10 被外部改写：文件从 1005 bytes / md5 `402f3c38…` 变成 136 bytes / md5 `c9409458…`，`ANTHROPIC_AUTH_TOKEN` 从 125 字符变成 11 字符占位串。三重佐证：① 绕过 ccc 直接 `claude --settings <同一文件> -p` 同样失败；② 用改写后的 token 直连 `https://api.minimaxi.com/anthropic/v1/messages` 返回 `HTTP 401 {"type":"error","error":{"type":"authentication_error","message":"login fail: …"}}`；③ 从源文件 `E:\__work\__frequently\personal_data\apikey\decrypt\cc_switcher\settings_minimax_m3.json` 重新按字节复制恢复后，单次调用立即恢复 `OK` / exit 0。**根因为共享机器上 profile 文件被并发改写（测试环境污染），与被测代码无关。** |
> | 第 2 次（复跑） | **50/50 通过，`TOTAL_FAIL=0`** | 运行前后 profile md5 恒为 `402f3c38…`（未被改写），`ccc` 进程数 before=0 / after=0，无句柄或进程泄漏。**以此为准。** |

---

## 3. 「明确不做的事」合规核查

验收标准明确要求「验收时不得出现」以下 6 项，实测均未出现：

| 禁止项 | 核查方法 | 实测 | 判定 |
|---|---|---|---|
| ❌ 不生成 account 隔离目录 | `find ~/.ccc` 全量列举 | 仅 `profiles/minimax_m3.json` + `sessions.json`，无任何隔离目录 | 合规 |
| ❌ 不改写 `CLAUDE_CONFIG_DIR` | `grep -nE "CLAUDE_CONFIG_DIR" *.go` | 源码中 0 处出现 | 合规 |
| ❌ 不注入 MCP server 或 hook | `grep -nE "mcpServers\|\"hooks\"" *.go` | 源码中 0 处出现（profile 内容原样透传，ccc 不解析） | 合规 |
| ❌ 不启动常驻进程/守护进程 | 验收全程进程快照 + 源码审查 | 无 goroutine/channel/daemon；50 次 headless 连跑前后 `ccc` 进程数均为 0 | 合规 |
| ❌ 不写 `~/.claude/` 下任何文件 | `find ~/.claude -newermt "2026-10-09 00:20" -type f \| wc -l` | **0** —— 整个验收窗口内 `~/.claude` 无任何文件被修改 | 合规 |
| ❌ exe 不依赖 bun/node/python | `go version -m` + `go.mod` + `go list -deps` | `CGO_ENABLED=0`；`go.mod` 无 `require`；非 std 依赖仅 `ccc` 自身 | 合规 |

---

## 4. 验收中发现的问题

### 🔴 B-1（阻断级 · 安全）— readme.md 提交并推送了真实 MiniMax API token

| 项 | 内容 |
|---|---|
| 位置 | `readme.md:234`（完整示例 JSON 内） |
| 泄露内容 | 真实可用的 `ANTHROPIC_AUTH_TOKEN`（`sk-cp-…`，长度 125），与验收期间 `~/.ccc/profiles/minimax_m3.json` 中正在使用的 token **完全相同**（sha256 前 12 位均为 `291bc07c113c`） |
| 传播范围 | 已 commit 于 `450c5aa`；`HEAD == origin/phase/0-scaffold == e70ddec98e9fd2d4b409e77f76e2397620636e72` ⇒ **已推送到 GitHub 远端** `git@github.com:burnlife001/ccc.git` |
| 后果 | 任何能访问该仓库的人都可取得该 token，直接盗用 MiniMax 额度 |

**建议处置顺序**：① 立即吊销/轮换该 token（仅 token 属主可执行）；② readme 示例替换为占位符；③ 复查 git 历史是否还有其他凭据；④ 若仓库为 public，改写历史无法撤销已泄露凭据 —— **轮换是唯一有效补救**。

已开 [YG-8](/YG/issues/YG-8) 收口。

### 🟡 B-2（非阻断 · 文档准确性）— readme 描述与实现不符

| readme 位置 | 文档描述 | 实现实际 |
|---|---|---|
| L158 | `ccc add` 默认 base_url = `https://api.anthropic.com` | `https://api.minimaxi.com/anthropic`（`cli.go:132`） |
| L160 | `ccc add` 默认 model = `claude-sonnet-4-5` | `MiniMax-M3`（`cli.go:149`） |
| L161 | `ccc add` 有第 4 问「permission mode / allow 列表 / haiku / opus 覆盖」 | 只问 3 个问题，无第 4 问（`cli.go:127-151`） |
| L134 | 无 session 时报错 `No previous session found for <profile>` | `ccc: no previous session for profile "x"; run 'ccc x -p "task"' first`（`cli.go:278`） |
| L183 / L195 / L273 | `ccc which` 输出 `claude --settings C:\...`（不带引号） | 实际每个参数都带双引号：`"…\claude.exe" "--settings" "C:\…"`（`cli.go:366-372`） |
| L92-101 | 标题「六个命令」但正文列 7 行、小节编号 1-7 | 标题与编号/内容不一致 |

不影响验收判定，但会误导用户。

### 🟡 B-3（非阻断 · 行为观察）— 交互模式 + 管道 stdin 时 claude 不退出，遗留进程树

| 项 | 内容 |
|---|---|
| 现象 | 4.2 测试后，发现 `ccc.exe minimax_m3` 进程存活 90 分钟未退出，其子进程 `claude-v2.1.294.exe --settings …` 同样存活 |
| 证据 | `PID 1328 (ccc.exe)` Created 00:38:16，`ParentPID 20636`（bash）**已死亡**；`HandleCount` 10 秒采样恒为 94（无增长）、累计 CPU 0.03s ⇒ **阻塞而非句柄泄漏** |
| 定性 | ccc 侧行为符合架构契约（交互模式 stdio 全部 inherit、`cmd.Run()` 等子进程自然退出后透传退出码）。滞留的是 claude 2.1.294 在 stdin 管道关闭后不退出，ccc 只是忠实等待 |
| 影响 | 50 次 headless 连跑（7.3）**未复现**，前后 `ccc` 进程数均为 0；仅交互 + 管道场景偶发 |
| 建议 | 记录为已知行为。若要根治，可在 `spawnInteractive` 中检测 stdin 非 TTY 且已 EOF 时主动结束子进程 |

### 🟢 观察项（非问题）

- claude 每次调用向 stderr 输出 `[claude-code:unrecognized_model] {"model":"MiniMax-M3","query_source":"sdk"}`。
  这是 Claude Code CLI 对未知模型名的提示，不影响调用（所有 API 测试均返回正确结果），属上游行为，ccc 按设计原样透传 stderr。
- `Warning: no stdin data received in 3s` 同理，为 claude 在无 stdin 场景下的自有提示。
- 验收窗口内 `~/.ccc/profiles/minimax_m3.json` 曾被外部并发改写（见 §2 的 7.3 执行记录）。
  **共享机器跑验收时，profile 文件应视为外部可变输入，建议每轮前后校验 md5。**

---

## 5. 复现步骤

```powershell
# 1. 构建 + 安装
cd E:\__work\__github\ccc
pwsh -NoProfile -ExecutionPolicy Bypass -File .\build.ps1
# 期望：[ok] built and installed version 0.1.0，无 error/warning

# 2. 准备 profile（验收标准前置条件）
mkdir -p $env:USERPROFILE\.ccc\profiles
Copy-Item E:\__work\__frequently\personal_data\apikey\decrypt\cc_switcher\settings_minimax_m3.json `
          $env:USERPROFILE\.ccc\profiles\minimax_m3.json

# 3. 冒烟
D:\Programs\ccc\ccc.exe version
D:\Programs\ccc\ccc.exe which minimax_m3
D:\Programs\ccc\ccc.exe minimax_m3 -p "只回复OK"
```

---

*报告由 小测 生成于 2026-10-09 · 依据 `docs/ccs验收标准.md` 逐条实测 · 未修改任何被测代码*
