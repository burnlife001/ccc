# ccc

> Claude Code profile switcher · Go 单文件 exe · 零运行时依赖 · kdbx 直读 · token 永不落盘

把多个 Claude Code 配置(不同 base_url / token / model)存进 KeePass `.kdbx`,
`ccc <profile>` 直接从 kdbx 解密并注入启动。**明文 token 永不落盘。**
也支持本地手动放置 profile json 作为 fallback。

---

## 目录

- [为什么用 ccc](#为什么用-ccc)
- [安装](#安装)
- [快速开始](#快速开始)
- [八个命令](#八个命令)
- [Profile JSON 字段说明](#profile-json-字段说明)
- [kdbx 直读机制](#kdbx-直读机制)
- [完整示例](#完整示例)
- [常见问题](#常见问题)
- [迁移自旧版 ccs](#迁移自旧版-ccs)

---

## 为什么用 ccc

| 痛点 | ccc 的做法 |
|---|---|
| 手改环境变量切 base_url/token | profile 存 kdbx,`ccc <name>` 一行搞定 |
| 切换时要拼接 `--settings ~/.xxx/yyy.json` | 自动从 kdbx 合成,用户无感知 |
| 子 shell 残留 `ANTHROPIC_*` 污染 | spawn 前自动剥掉父进程所有 `ANTHROPIC_*` |
| npm/Node 包要装运行时 | 单个 exe,< 6 MB,无需 Go/Node/Python |
| **token 明文 json 散落各处** | **kdbx 是唯一真相,内存解密,临时文件,用完即删** |
| 想调试最终跑了什么命令 | `ccc which <name>` 打印完整命令行 |

不做的事(刻意):不建 account 隔离目录、不改写 `CLAUDE_CONFIG_DIR`、不注入 MCP/hook、不写 `~/.claude/`、不起守护进程。

---

## 安装

### 前置

- Windows 11
- Claude Code CLI 已装好(`claude --version` 能跑)
- Go ≥ 1.22(仅编译时需要,运行时不依赖)
- **必需**:一份 KeePass `.kdbx`,主密码已存进 Windows Credential Manager(target=`apikey_kdbx_master`)

### 三步装好

```bash
# 1. 拉代码
git clone https://github.com/burnlife001/ccc.git
cd ccc

# 2. 编译(产物在 dst/,单 exe 约 4 MB,不安装、不改 PATH)
./build.ps1

# 3. 验证
./dst/ccc.exe version
```

`build.ps1` 只做一件事:`go build -ldflags "-s -w" -o dst/ccc.exe ./src`。

想在任意目录直接敲 `ccc`,自己把 `dst/` 加进 PATH 或做个软链即可,脚本不代劳。

### 数据目录在哪

**与 ccc.exe 同级的 `.ccc/`**——本仓库即 `dst/.ccc/`:

```
dst/
├── ccc.exe
└── .ccc/
    ├── profiles/<name>.json   # 本地手动放置的 profile(fallback/调试用,可选)
    └── (无 sessions.json)     # 不维护本地 session 状态;续接直接传 claude -c
```

exe 和数据同生命期,整个 `dst/` 目录拷走即可搬运。`dst/.ccc/` 已在 `.gitignore`。
测试要隔离数据时设 `CCC_HOME=<目录>`,`.ccc/` 就会落到那个目录下。

**注意**:`profiles/` 目录是**可选的**。没有它,ccc 也能跑——所有 profile 都从 kdbx 读。

---

## 快速开始

### 前提:kdbx 里已有数据

你的 `.kdbx` 里应该有一个组叫 `cc`,组里每条 entry 是一个供应商(如 `minimax`、`kimi`):

| kdbx 字段 | 含义 |
|---|---|
| `title` | 供应商名(如 `minimax`),只是标签 |
| `password` | 该供应商的 API token |
| `notes` | **ccp-v1 格式的 JSON**,声明 `base_url` + `profiles{}`(每个 profile 对应一份模型配置) |

notes 示例:

```json
{
  "base_url": "https://api.minimaxi.com/anthropic",
  "profiles": {
    "minimax_m3":           { "model": "MiniMax-M3" },
    "minimax_m3_highspeed": { "model": "MiniMax-M3-highspeed" }
  }
}
```

这条 entry 会展开成 2 个可用 profile:`minimax_m3` 和 `minimax_m3_highspeed`。

### 存主密码(只需一次)

```powershell
pwsh -File E:\__work\__frequently\personal_data\apikey\decrypt\cc_switcher\test_kdbx_master_cred.ps1 -Mode Store
```

### 使用

```bash
# 列出所有可用 profile(本地 + kdbx)
./dst/ccc.exe list

# 启动交互式会话
./dst/ccc.exe minimax_m3

# 单轮任务
./dst/ccc.exe minimax_m3 -p "你好"
```

不熟 profile 文件长啥样?往下翻到 [完整示例](#完整示例)。

---

## 六个命令

```
ccc <profile>           # 启动交互式 claude 会话
ccc <profile> -p "任务" # 单轮 headless 任务
ccc <profile> -c -p "任务" # 续接上次会话(claude 自带 -c,无需 ccc 记 state)
ccc list                # 列出所有 profile(本地 + kdbx)
ccc which <profile>     # 打印将执行的命令(调试用,不实际启动)
ccc version             # 输出版本号
```

### 详细行为

#### 1. `ccc <profile>` — 交互启动

```
ccc minimax_m3
```

**查找顺序**:
1. 本地 `dst/.ccc/profiles/minimax_m3.json` — 存在则直接用
2. kdbx `cc` 组 — 遍历每条 entry 的 `notes.profiles{}`,找到 key 为 `minimax_m3` 的那条,**内存合成 settings JSON**,写入临时文件

- 拼出 `claude --settings <临时文件路径> [你传的任何参数...]`
- spawn 前**剥掉**父进程所有 `ANTHROPIC_*` 环境变量(防嵌套污染)
- stdio 全部 inherit(支持 `echo 测试 | ccc minimax_m3`)
- 退出码透传:Claude Code 的退出码就是 ccc 的退出码
- claude 退出后,**临时文件立即删除**

#### 2. `ccc <profile> -p "任务"` — 单轮 headless

```
ccc minimax_m3 -p "回复 OK 两个字"
```

- 同上,但走 stream-json 输出路径
- 输出为纯文本(已剥 stream-json 信封),`exit code 0` 即成功

> 想续接上一轮?直接 `ccc <profile> -c -p "..."` —— claude 自带 `-c` 续接最近一次会话,ccc 不维护 session 状态。

#### 3. `ccc list`

```
ccc list
```

输出:

```
[kdbx]   minimax_m3      → kdbx:cc/minimax (notes.profiles.minimax_m3)
[kdbx]   minimax_m3_hs   → kdbx:cc/minimax (notes.profiles.minimax_m3_highspeed)
```

#### 4. `ccc which <profile>` — 调试利器

```
ccc which minimax_m3
```

输出完整命令行,**路径全是绝对路径**:

kdbx 源:
```
[kdbx]  "C:\Users\yg\.local\bin\claude.exe" "--settings" "C:\Users\yg\AppData\Local\Temp\ccc-settings-123456789.json"
```

支持透传参数:

```
ccc which minimax_m3 --max-turns 5
```

→

```
[kdbx]  "C:\Users\yg\.local\bin\claude.exe" "--settings" "C:\Users\yg\AppData\Local\Temp\ccc-settings-123456789.json" "--max-turns" 5
```

#### 5. `ccc version`

```
ccc version
```

输出 `ccc 0.1.0` 这种语义化版本号。

---

## Profile JSON 字段说明

ccc **不做字段校验**——profile 文件就是 `--settings` 的内容,Claude Code 怎么消费,ccc 就怎么传。下面这张表是"常见字段参考",**不**是"必填清单"。少写/多写字段都不会让 ccc 报错。

| 路径 | 含义 | 示例值 | 备注 |
|---|---|---|---|
| `env.ANTHROPIC_BASE_URL` | Anthropic 兼容 API 的 base URL | `https://api.minimaxi.com/anthropic` | 切模型必改 |
| `env.ANTHROPIC_AUTH_TOKEN` | API key(token) | `sk-cp-xxxxxxxxxxxx` | **kdbx 源时永不落盘** |
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

## kdbx 直读机制

### 为什么这样设计

传统方案:token 明文存在 json 里,一份 profile 一份 token,难管、难轮换、易泄露。

ccc 方案:**kdbx 是唯一真相**。token 只在 kdbx 里存一份(强加密),ccc 启动时:
1. 从 Windows Credential Manager 取主密码
2. 解密 kdbx,找到对应 entry
3. **内存中合成**完整 settings JSON(含 token)
4. 写入**临时文件**(0600,仅当前用户可读)
5. spawn claude,传临时文件路径
6. claude 退出后,**立即删除临时文件**

**明文 token 在磁盘上的存活时间 = claude 单次运行的时间。** 进程退出,文件消失。

### 性能

kdbx 解密有固定开销(~200–500 ms,KDF 计算)。对交互式启动可接受;对 `-p` 高频调用(如自动化脚本)会明显变慢。

**高频场景建议**:把 profile JSON 手动放到 `dst/.ccc/profiles/`(明文),作为 fallback。kdbx 是主源,本地是缓存。

### 离线场景

kdbx 文件所在盘未挂载时:
- 本地 `profiles/` 里有同名 profile → 自动用本地
- 没有 → 报错明确提示"kdbx 不可达,可用本地 profile 或检查路径"

### 安全边界

| 威胁 | 防护 |
|---|---|
| 误 commit 到 git | ✅ `dst/.ccc/` gitignored;临时文件在 `%TEMP%` |
| 其它 Windows 用户读 | ✅ 临时文件 0600;kdbx 本身有 ACL |
| 恶意软件同用户扫盘 | ❌ 同用户进程都能读——这是 Windows 桌面环境的物理上限 |
| 磁盘被拔走克隆 | ❌ 同上;建议全盘 BitLocker |

---

## 完整示例

### kdbx 里的 entry

```
Title:    minimax
Password: sk-cp-xxxxxxxxxxxx
Notes:
{
  "base_url": "https://api.minimaxi.com/anthropic",
  "profiles": {
    "minimax_m3": {
      "model": "MiniMax-M3",
      "haiku": "MiniMax-M3",
      "opus": "MiniMax-M3-highspeed",
      "sonnet": "MiniMax-M3"
    }
  },
  "extra_env": {
    "CUSTOM_FLAG": "1"
  }
}
```

### ccc 合成的 settings JSON(内存中,不落盘)

```json
{
  "env": {
    "ANTHROPIC_AUTH_TOKEN": "sk-cp-xxxxxxxxxxxx",
    "ANTHROPIC_BASE_URL": "https://api.minimaxi.com/anthropic",
    "ANTHROPIC_DEFAULT_HAIKU_MODEL": "MiniMax-M3",
    "ANTHROPIC_DEFAULT_OPUS_MODEL": "MiniMax-M3-highspeed",
    "ANTHROPIC_DEFAULT_SONNET_MODEL": "MiniMax-M3",
    "ANTHROPIC_MODEL": "MiniMax-M3",
    "CUSTOM_FLAG": "1",
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

### 本地 fallback profile(可选)

文件路径:`E:\__work\__github\ccc\dst\.ccc\profiles\minimax_m3.json`

内容同上,但**明文落盘**。用于:
- 离线场景(kdbx 不在)
- 高频 `-p` 调用(省 kdbx 解密开销)
- 调试/实验

> ⚠️ **安全**
> - 本地 profile 是**明文**;从 kdbx 解密到临时文件时已设 `0600`,手动放入的 profile 请自行保证权限。
> - `dst/.ccc/` 已整体进 `.gitignore`——但**别手动把它加回来**。
> - 更稳的做法是只用 kdbx:token 只在 kdbx 里存一份,内存解密,用完即删。

### 完整使用流程

```bash
# 1. 列出所有 profile
./dst/ccc.exe list
# [kdbx]   minimax_m3      → kdbx:cc/minimax (notes.profiles.minimax_m3)

# 2. 看命令对不对(不实际启动)
./dst/ccc.exe which minimax_m3
# [kdbx]  "C:\Users\yg\.local\bin\claude.exe" "--settings" "C:\...\Temp\ccc-settings-....json"

# 3. 进交互
./dst/ccc.exe minimax_m3
# 会话里 /status 应能看到 ANTHROPIC_BASE_URL=https://api.minimaxi.com/anthropic

# 4. 单轮
./dst/ccc.exe minimax_m3 -p "只回 OK"
# OK

# 5. 续接
./dst/ccc.exe minimax_m3 -p "记住数字 8741,只回'收到'"
./dst/ccc.exe minimax_m3 -c -p "我让你记的数字?"
# 8741
```

---

## 常见问题

### `ccc` 不被识别?

ccc 不安装、不改 PATH,产物就是仓库里的 `dst/ccc.exe`。两种解决:
1. 用全路径或相对路径调用,如 `./dst/ccc.exe version`
2. 自己把 `dst/` 加进 PATH(系统属性 → 高级 → 环境变量 → `Path` → 新建)

### `profile not found: xxx`

- 本地 `dst/.ccc/profiles/xxx.json` 不存在
- kdbx `cc` 组里也没有 `profiles{}` 的 key 为 `xxx` 的 entry
- 用 `ccc list` 看现有名字

### 父进程的 `ANTHROPIC_AUTH_TOKEN` 没生效?

这是**故意的**。ccc 在 spawn 之前会清掉父进程所有 `ANTHROPIC_*` 变量,这样 profile 里的值不会被嵌套污染。`/status` 里看到的 token 一定来自 kdbx(或本地 profile 文件),不是你的 shell 环境变量。

### `ccc` 报 `CRED_NOT_FOUND`

主密码没存进 Windows Credential Manager。跑一次:

```powershell
pwsh -File E:\__work\__frequently\personal_data\apikey\decrypt\cc_switcher\test_kdbx_master_cred.ps1 -Mode Store
```

### `ccc` 报 `kdbx: bad credentials`

Credential 里存的密码跟 kdbx 当前密码不一致了——可能你最近改过 kdbx 主密码。重新跑一次上面的 `-Mode Store`。

### `ccc` 报 `kdbx: group "cc" not found`

你的 kdbx 里没有叫 `cc` 的组。检查 kdbx 结构,或设 `CCC_KDBX_GROUP=<你的组名>`。

### 想看 ccc 实际跑了什么命令?

`ccc which <profile>` 打印完整命令行,带绝对路径。

### 老的 `cc_switcher_kdbx.ps1` 还要吗

不用了。ccc 已接管所有职责;老脚本在原路径留作参考实现,不再维护。

---

## 迁移自旧版 ccs

如果你已经在用 TypeScript 版 ccs(`E:/__work/__github/ccs/`),所有 profile json 是**字节级兼容**的——直接复制到 `dst/.ccc/profiles/` 即可作为本地 fallback 用,不用改任何字段。

但推荐做法是把 token 收进 kdbx,享受"永不落盘"的好处。

详见 [docs/MIGRATION.md](docs/MIGRATION.md)。

---

## 项目结构

```
ccc/
├── src/
│   ├── main.go        # 唯一入口:错误处理 + 退出码
│   ├── cli.go         # 命令分发:8 个子命令 + 透传分支
│   ├── exec.go        # 唯一允许 os/exec 的文件
│   ├── profiles.go    # 本地 profile CRUD + 内存临时文件管理
│   ├── paths.go       # 路径常量集中
│   └── kdbx.go        # 唯一允许 import gokeepasslib + advapi32 的文件:kdbx 直读
├── dst/
│   ├── ccc.exe        # 构建产物(build.ps1 生成)
│   └── .ccc/          # 运行时数据(profiles[fallback] + sessions,gitignored)
├── docs/
│   ├── go架构规范.md      # 架构契约(为什么这么写)
│   ├── ccs验收标准.md     # 完整验收矩阵
│   ├── todolist.md        # 阶段开发计划
│   ├── MIGRATION.md       # 旧版 ccs → ccc 迁移指南
│   └── ccs-lite-reference/  # TypeScript 旧版关键源
├── build.ps1      # go build ./src → dst/ccc.exe(不安装)
└── readme.md      # 本文件
```

架构硬约束见 [docs/go架构规范.md](docs/go架构规范.md)(六文件、四层、仅 gokeepasslib 一个破例依赖、无 goroutine)。

---

## License

MIT
