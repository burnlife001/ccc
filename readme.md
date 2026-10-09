# ccc

> Claude Code profile switcher · Go 单文件 exe · 零运行时依赖

把多个 Claude Code 配置（不同 base_url / token / model）存成一份份 profile json,
一个 `ccc <profile>` 就能切到对应模型启动。零字段校验,profile 文件就是 `--settings`,
复制即迁移。

---

## 目录

- [为什么用 ccc](#为什么用-ccc)
- [安装](#安装)
- [快速开始](#快速开始)
- [八个命令](#八个命令)
- [Profile JSON 字段说明](#profile-json-字段说明)
- [完整示例](#完整示例)
- [常见问题](#常见问题)
- [迁移自旧版 ccs](#迁移自旧版-ccs)

---

## 为什么用 ccc

| 痛点 | ccc 的做法 |
|---|---|
| 手改环境变量切 base_url/token | profile 文件,一份对应一个模型 |
| 切换时要拼接 `--settings ~/.xxx/yyy.json` | `ccc <name>` 一行搞定 |
| 子 shell 残留 `ANTHROPIC_*` 污染 | spawn 前自动剥掉父进程所有 `ANTHROPIC_*` |
| npm/Node 包要装运行时 | 单个 exe,< 10 MB,无需 Go/Node/Python |
| 想调试最终跑了什么命令 | `ccc which <name>` 打印完整命令行 |

不做的事(刻意):不建 account 隔离目录、不改写 `CLAUDE_CONFIG_DIR`、不注入 MCP/hook、不写 `~/.claude/`、不起守护进程。

---

## 安装

### 前置

- Windows 11
- Claude Code CLI 已装好(`claude --version` 能跑)
- Go ≥ 1.22(仅编译时需要,运行时不依赖)

### 三步装好

```bash
# 1. 拉代码
git clone https://github.com/burnlife001/ccc.git
cd ccc

# 2. 编译(脚本里会 go build + 复制到 D:/Programs/ccc/ + 加 PATH)
./build.ps1

# 3. 验证
ccc version
```

`build.ps1` 做了什么:

1. `go build -ldflags "-s -w" -o dst/ccc.exe ./src`(产出 < 10 MB 的单 exe)
2. 复制到 `D:\Programs\ccc\ccc.exe`
3. 把 `D:\Programs\ccc\` 加进 PATH(如果还没加)

> 装好后**新开一个终端**才能 `ccc` 直接跑。不想加 PATH?直接用 `D:\Programs\ccc\ccc.exe <args>` 全路径也行。

---

## 快速开始

把最小化的 profile 文件丢到 `~/.ccc/profiles/` 就能用。

```bash
# 假设你已经有 ~/.ccs/settings_minimax_m3.json(从旧版 ccs 复制过来)
mkdir -p ~/.ccc/profiles
cp ~/.ccs/settings_minimax_m3.json ~/.ccc/profiles/minimax_m3.json

# 启动交互式会话
ccc minimax_m3

# 一句话任务
ccc minimax_m3 -p "你好"
```

不熟 profile 文件长啥样?往下翻到 [完整示例](#完整示例)。

---

## 八个命令

```
ccc <profile>           # 启动交互式 claude 会话
ccc <profile> -p "任务" # 单轮 headless 任务
ccc <profile>:continue  # 继续上一次的会话(读取上次存的 session_id)
ccc list                # 列出所有 profile
ccc add <name>          # 交互式生成新 profile
ccc rm <name>           # 删除 profile
ccc which <profile>     # 打印将执行的命令(调试用,不实际启动)
ccc version             # 输出版本号
```

### 详细行为

#### 1. `ccc <profile>` — 交互启动

```
ccc minimax_m3
```

- 找到 `~/.ccc/profiles/minimax_m3.json` → 校验"存在 + 合法 JSON"
- 拼出 `claude --settings <绝对路径> [你传的任何参数...]`
- spawn 前**剥掉**父进程所有 `ANTHROPIC_*` 环境变量(防嵌套污染)
- stdio 全部 inherit(支持 `echo 测试 | ccc minimax_m3`)
- 退出码透传:Claude Code 的退出码就是 ccc 的退出码

#### 2. `ccc <profile> -p "任务"` — 单轮 headless

```
ccc minimax_m3 -p "回复 OK 两个字"
```

- 同上,但走 stream-json 输出路径
- 内部捕获 `session_id` 存到 `~/.ccc/sessions.json`(键是 profile 名),给下次 `:continue` 用
- 输出为纯文本(已剥 stream-json 信封),`exit code 0` 即成功

#### 3. `ccc <profile>:continue` — 续接上次会话

```
ccc minimax_m3:continue -p "我刚才让你记的数字?"
```

- 从 `~/.ccc/sessions.json` 读出 profile 对应的 `session_id`
- 没有 session_id?明确报错 `ccc: no previous session for profile "<name>"; run \`ccc <name> -p "task"\` first`,exit code ≠ 0
- 拼 `--resume <sessionId>` 透传给 claude

#### 4. `ccc list`

```
ccc list
```

输出:

```
minimax_m3 → C:\Users\yg\.ccc\profiles\minimax_m3.json
test_p     → C:\Users\yg\.ccc\profiles\test_p.json
```

#### 5. `ccc add <name>`

```
ccc add my_gpt
```

进入交互式提问(顺序):

1. `base_url`(默认 `https://api.minimaxi.com/anthropic`)
2. `token`(留空 = 依次从 `$ANTHROPIC_AUTH_TOKEN` / `$ANTHROPIC_API_KEY` 环境变量读)
3. `model`(默认 `MiniMax-M3`)

生成 `~/.ccc/profiles/my_gpt.json`(模板见 [完整示例](#完整示例))。

#### 6. `ccc rm <name>`

```
ccc rm test_p
```

- 删 `~/.ccc/profiles/test_p.json`
- 不存在 → 报错 `profile not found: test_p`,exit code ≠ 0,不删错文件

#### 7. `ccc which <profile>` — 调试利器

```
ccc which minimax_m3
```

输出完整命令行,**路径全是绝对路径**(无 `~`、无相对路径):

```
"C:\Users\yg\.local\bin\claude.exe" "--settings" "C:\Users\yg\.ccc\profiles\minimax_m3.json"
```

支持透传参数:

```
ccc which minimax_m3 --max-turns 5
```

→

```
"C:\Users\yg\.local\bin\claude.exe" "--settings" "C:\Users\yg\.ccc\profiles\minimax_m3.json" "--max-turns" 5
```

---

## Profile JSON 字段说明

ccc **不做字段校验**——profile 文件就是 `--settings` 的内容,Claude Code 怎么消费,ccc 就怎么传。下面这张表是"常见字段参考",**不**是"必填清单"。少写/多写字段都不会让 ccc 报错。

| 路径 | 含义 | 示例值 | 备注 |
|---|---|---|---|
| `env.ANTHROPIC_BASE_URL` | Anthropic 兼容 API 的 base URL | `https://api.minimaxi.com/anthropic` | 切模型必改 |
| `env.ANTHROPIC_AUTH_TOKEN` | API key(token) | `sk-cp-xxxxxxxxxxxx` | **会落盘**,文件权限请自己管 |
| `env.ANTHROPIC_MODEL` | 当前使用的模型 | `MiniMax-M3` | |
| `env.ANTHROPIC_DEFAULT_HAIKU_MODEL` | 小模型覆盖 | `MiniMax-M3` | 可选 |
| `env.ANTHROPIC_DEFAULT_OPUS_MODEL` | 大模型覆盖 | `MiniMax-M3-highspeed` | 可选 |
| `env.ANTHROPIC_DEFAULT_SONNET_MODEL` | 中模型覆盖 | `MiniMax-M3` | 可选 |
| `env.ANTHROPIC_SMALL_FAST_MODEL` | 小模型别名 | `MiniMax-M3` | 旧字段,部分场景需要 |
| `env.CLAUDE_CODE_DISABLE_NONESSENTIAL_TRAFFIC` | 关闭非必要流量 | `"1"` | 减少噪音 |
| `env.DISABLE_TELEMETRY` | 关掉遥测 | `"1"` | |
| `env.CLAUDE_CODE_USE_POWERSHELL_TOOL` | 用 PowerShell 跑 Bash | `"1"` | Windows 推荐开 |
| `env.CLAUDE_CODE_MAX_CONTEXT_TOKENS` | 上下文 token 上限 | `"1000000"` | |
| `env.CLAUDE_CODE_AUTO_COMPACT_WINDOW` | 自动压缩窗口 | `"500000"` | |
| `permissions.defaultMode` | 权限模式 | `bypassPermissions` / `acceptEdits` / `default` | `bypassPermissions` 最省事 |
| `permissions.allow` | 允许的工具白名单 | `["Bash","Edit","Read",...]` | 数组 |
| `permissions.deny` | 禁用工具黑名单 | `["WebFetch"]` | 数组,可选 |
| `skipDangerousModePermissionPrompt` | 跳过危险模式确认 | `true` | 配 `bypassPermissions` 用 |
| `mcpServers` | MCP server 配置 | 见 Claude Code 文档 | ccc **不解析此字段**,原样透传 |
| `hooks` | hooks 配置 | 见 Claude Code 文档 | 同上,原样透传 |

---

## 完整示例

文件路径:`C:\Users\yg\.ccc\profiles\minimax_m3.json`

```json
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "sk-cp-REDACTED-EXAMPLE",
    "ANTHROPIC_BASE_URL": "https://api.minimaxi.com/anthropic",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "MiniMax-M3",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "MiniMax-M3-highspeed",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "MiniMax-M3",
    "ANTHROPIC_MODEL": "MiniMax-M3",
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
```

> ⚠️ **安全**
> - token 是明文 JSON,文件权限请自己管。Windows 上文件默认只对当前用户可读。
> - 别把 profile 文件 commit 到公开仓库。
> - 想用环境变量而不是落盘?删掉 `ANTHROPIC_AUTH_TOKEN` 那一行,Claude Code 会自己从 `ANTHROPIC_AUTH_TOKEN` 环境变量读——但 ccc spawn 前会**剥掉**父进程的这个变量,所以请走 Claude Code 自己识别的机制。

完整使用流程:

```bash
# 1. 把文件存好
mkdir -p ~/.ccc/profiles
cat > ~/.ccc/profiles/minimax_m3.json <<'EOF'
{ ... 上面的内容 ... }
EOF

# 2. 看命令对不对(不实际启动)
ccc which minimax_m3
# 输出:
# "C:\Users\yg\.local\bin\claude.exe" "--settings" "C:\Users\yg\.ccc\profiles\minimax_m3.json"

# 3. 进交互
ccc minimax_m3
# 会话里 /status 应能看到 ANTHROPIC_BASE_URL=https://api.minimaxi.com/anthropic

# 4. 单轮
ccc minimax_m3 -p "只回 OK"
# OK
```

---

## 常见问题

### `ccc` 不被识别?

PATH 里没 `D:\Programs\ccc\`。两种解决:
1. 重开终端(`build.ps1` 加的 PATH 只对新进程生效)
2. 手动加:系统属性 → 高级 → 环境变量 → `Path` → 新建 → `D:\Programs\ccc`

### `profile not found: xxx`

- 文件不存在于 `~/.ccc/profiles/xxx.json`
- 用 `ccc list` 看现有名字
- 文件名大小写敏感(Windows 默认不敏感,但 ccc 走文件系统,会按你写的大小写存)

### 父进程的 `ANTHROPIC_AUTH_TOKEN` 没生效?

这是**故意的**。ccc 在 spawn 之前会清掉父进程所有 `ANTHROPIC_*` 变量,这样 profile 里的值不会被嵌套污染。`/status` 里看到的 token 一定来自 profile 文件,不是你的 shell 环境变量。

### 6.x 验收的 `-p` 输出是乱码?

正常情况下不会。ccc 内部会剥掉 stream-json 信封输出纯文本。如果你看到 `{"type":"assistant",...}` 这种 JSON,说明上游 `-p` 的实现还没合上,请检查 `docs/ccs验收标准.md` P1 第 6 节。

### 想看 ccc 实际跑了什么命令?

`ccc which <profile>` 打印完整命令行,带绝对路径。

---

## 迁移自旧版 ccs

如果你已经在用 TypeScript 版 ccs(`E:/__work/__github/ccs/`),所有 profile json 是**字节级兼容**的——直接复制即可,不用改任何字段。

详见 [docs/MIGRATION.md](docs/MIGRATION.md)。

---

## 项目结构

```
ccc/
├── src/
│   ├── main.go        # 唯一入口:错误处理 + 退出码
│   ├── cli.go         # 命令分发:7 个子命令 + 透传分支
│   ├── exec.go        # 唯一允许 exec.Command 的文件
│   ├── profiles.go    # profile 文件读写(纯文件操作)
│   └── paths.go       # 路径常量集中
├── dst/
│   └── ccc.exe      # 构建产物(build.ps1 生成)
├── docs/
│   ├── go架构规范.md      # 架构契约(为什么这么写)
│   ├── ccs验收标准.md     # 完整验收矩阵
│   ├── todolist.md        # 阶段开发计划
│   ├── MIGRATION.md       # 旧版 ccs → ccc 迁移指南
│   └── ccs-lite-reference/  # TypeScript 旧版关键源
├── build.ps1      # go build + 安装到 D:\Programs\ccc
└── readme.md      # 本文件
```

架构硬约束见 [docs/go架构规范.md](docs/go架构规范.md)(五文件、四层、std-only、无 goroutine)。

---

## License

MIT