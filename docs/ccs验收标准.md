# ccc 验收标准

> 验收对象：Go 单 exe 版 ccc（见 `todolist.md`）。
> 验收方法：每条标准给出「操作 → 预期结果 → 证据」。所有命令在 Windows 11 + pwsh/git-bash 下执行。
> 判定规则：P0 全部通过 = 可交付；P1 全部通过 = 可替换旧 ccs；P2 通过 = 加分项。
> 验收前准备：`claude --version` 可用；`~/.ccc/profiles/` 下已有 `minimax_m3.json`（内容同 `reference/settings_minimax_m3.template.json`，token 已填）。

## P0 — 核心透传（不通过则整体不合格）

### 1. 安装与启动

| # | 操作 | 预期结果 | 证据 |
|---|---|---|---|
| 1.1 | 运行 build.ps1 后 `ccc version` | 输出语义化版本号（如 `ccc 0.1.0`），exit code 0 | 终端截图/复制输出 |
| 1.2 | 查看 `D:\Programs\ccc\ccc.exe` | 单文件存在，体积 < 10 MB，无伴随 dll/runtime 目录 | 文件属性 |
| 1.3 | 双击/直接运行 exe（无参数） | 打印用法说明 + exit code 非 0，不 panic | 终端输出 |
| 1.4 | `ccc <不存在的profile>` | 明确报错 `profile not found: xxx` + 提示可用列表，exit code 非 0 | 终端输出 |

### 2. 交互模式透传

| # | 操作 | 预期结果 | 证据 |
|---|---|---|---|
| 2.1 | `ccc minimax_m3` | 进入 claude 交互界面，模型显示 MiniMax-M3 | 界面截图 |
| 2.2 | 在会话中执行 `/status`（或 `/model`） | 显示 `ANTHROPIC_BASE_URL=https://api.minimaxi.com/anthropic`，证明 `--settings` 生效 | 界面输出 |
| 2.3 | 在会话中提问"读取我全局 CLAUDE.md 里关于编码的约定" | 能复述 `~/.claude/CLAUDE.md` 内容（证明全局 CLAUDE.md 加载） | 对话记录 |
| 2.4 | 在会话中输入 `/exit` | 退出后 shell 拿到 claude 的退出码 | echo $? |
| 2.5 | `echo 测试 | ccc minimax_m3`（管道 stdin） | claude 正常收到 stdin 内容 | 行为观察 |

### 3. 参数透传

| # | 操作 | 预期结果 | 证据 |
|---|---|---|---|
| 3.1 | `ccc minimax_m3 --version` | 透传成功，输出 claude 版本（证明未知 flag 原样追加） | 终端输出 |
| 3.2 | `ccc which minimax_m3` | 打印完整命令行：`claude --settings <绝对路径>minimax_m3.json`，路径无相对/无 `~` | 终端输出 |
| 3.3 | `ccc which minimax_m3 --max-turns 5` | 打印的命令行末尾含 `--max-turns 5` | 终端输出 |

### 4. 环境隔离

| # | 操作 | 预期结果 | 证据 |
|---|---|---|---|
| 4.1 | pwsh 中 `$env:ANTHROPIC_AUTH_TOKEN="污染值"; ccc which minimax_m3` 后启动会话 `/status` | 会话实际使用的 token 来自 profile 文件而非污染值（证明 spawn 前 strip 了父进程 `ANTHROPIC_*`） | /status 输出 |
| 4.2 | git-bash 与 pwsh 各跑一次 2.1 | 两者行为一致 | 双终端截图 |

## P1 — profile 管理

| # | 操作 | 预期结果 | 证据 |
|---|---|---|---|
| 5.1 | `ccc list` | 列出 profiles 目录全部 json（含 minimax_m3），格式 `名称 → 文件路径` | 终端输出 |
| 5.2 | `ccc add test_p`（交互输入 base_url/token/model） | 生成 `~/.ccc/profiles/test_p.json`；文件是合法 JSON；token 落盘；再次 `list` 可见 | 文件内容 + list 输出 |
| 5.3 | `ccc which test_p` 并实际启动 | 路由到 add 时填入的 base_url（`/status` 核对） | /status 输出 |
| 5.4 | `ccc rm test_p` | 文件删除，`list` 不再出现；对不存在的 profile 报 `not found` 而非 panic | 终端输出 |
| 5.5 | profile json 含语法错误时 `ccc badprofile` | 明确报错并指出文件路径，exit code 非 0 | 终端输出 |

## P1 — `-p` 与 continue

| # | 操作 | 预期结果 | 证据 |
|---|---|---|---|
| 6.1 | `ccc minimax_m3 -p "回复OK两个字"` | 输出 `OK`；exit code 0；输出为纯文本（非 stream-json 乱码） | 终端输出 |
| 6.2 | `ccc minimax_m3 -p "记住数字 8741。只回复收到"` | 回复收到 | 终端输出 |
| 6.3 | 紧接着 `ccc minimax_m3:continue -p "我让你记的数字是？"` | 回答 `8741`（证明 session_id 捕获与 `--resume` 生效） | 终端输出 |
| 6.4 | `ccc minimax_m3 -p "任务" --max-turns 1` | 参数生效，一轮即停 | 行为观察 |
| 6.5 | 断网状态下执行 6.1 | 报错信息可读（claude 原生报错透传），exit code 非 0 | 终端输出 |

## P2 — 收尾

| # | 操作 | 预期结果 | 证据 |
|---|---|---|---|
| 7.1 | 把 `~/.ccs/settings_minimax_m3.json` 移入 `~/.ccc/profiles/` 后 `ccc minimax_m3 -p "OK?"` | 旧数据零改动直接可用 | 终端输出 |
| 7.2 | readme.md | 含：安装步骤、`list/add/rm/which/<profile>/<profile>:continue` 六个命令、profile json 字段表、一个完整示例 | 文件审查 |
| 7.3 | 连续执行 6.1 五十次（简单循环） | 无句柄/进程泄漏，全部 exit code 0 | 循环脚本输出 |

## 明确不做的事（验收时不得出现）

- ❌ 不生成 account 隔离目录、不做 `CLAUDE_CONFIG_DIR` 改写
- ❌ 不注入任何 MCP server 或 hook
- ❌ 不启动任何常驻进程/守护进程
- ❌ 不写 `~/.claude/` 下任何文件
- ❌ exe 不依赖 bun/node/python 运行时
