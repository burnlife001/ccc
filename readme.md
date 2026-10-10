# ccc

> Claude Code 一键切供应商 · Go 单 exe · token 永不落盘

把 Claude Code 的多套配置(不同 `base_url` / token / 模型)集中存进 KeePass `.kdbx`,
`ccc <profile>` 直接从 kdbx 解密启动。**明文 token 永不写盘。**

---

## 适合谁

- 在 MiniMax / Kimi / DeepSeek / Claude 之间切来切去,不想手改环境变量
- 一供应商多模型(如 MiniMax 配 M3 / M3-highspeed),想按需切
- token 不愿散落 json,想集中加密保管
- 想单 exe 跑,不装 npm / Node / Python

不做什么(刻意):不改 `CLAUDE_CONFIG_DIR`、不注入 MCP/hook、不写 `~/.claude/`、不起守护进程。

---

## 30 秒上手

```bash
# 编译(产物在 dst/ccc.exe,~4MB,不改 PATH、不装)
./build.ps1

# 首次:交互式存 kdbx 主密码 → DPAPI 加密落盘
./dst/ccc.exe cred store

# 看可用 profile
./dst/ccc.exe list

# 启动
./dst/ccc.exe minimax_m3            # 交互
./dst/ccc.exe minimax_m3 -p "你好"  # 单轮
```

**前置**:Windows 11 · `claude` CLI 已装 · Go ≥ 1.22(只编译需要)· kdbx 已建好且主密码已通过 `ccc cred store` 加密落盘。

---

## 7 个命令

| 命令 | 用途 |
|---|---|
| `ccc <profile>` | 启动交互会话 |
| `ccc <profile> -p "任务"` | 单轮 headless 任务 |
| `ccc <profile> -c -p "任务"` | 续接上次会话(claude 原生 `-c`) |
| `ccc list` | 列出所有 profile |
| `ccc which <profile>` | 打印将执行的完整命令(调试用,不实际启动) |
| `ccc cred <store\|check\|delete>` | 管理 kdbx 主密码(DPAPI 加密落盘) |
| `ccc version` | 输出版本号 |

`ccc` 后的额外参数都会原样透传给 `claude`。

---

## kdbx 怎么读

在 kdbx 里建一个叫 `cc` 的组,每条 entry = 一个供应商:

- **title**: 供应商标签(如 `minimax`)
- **password**: 这个供应商的 API token
- **notes**: 一段 JSON,声明 `base_url` + `profiles{}`,每个 key = 一个可用 profile 名

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

启动时:DPAPI 凭据文件取主密码 → 解 kdbx → 内存合成 settings JSON → 写临时文件 → 启 claude → 退出即删临时文件。
**token 在磁盘上的存活时间 ≈ claude 一次运行的时间。**

也可以手动把 profile json 放进 `dst/.ccc/profiles/<name>.json` 做 fallback(明文,适合离线 / 高频 `-p`)。

## apikey.kdbx.example中的模型ID：
ccc 580ai
ccc Kimi_k28
ccc Kimi_k3
ccc deepseek_flash
ccc deepseek_pro
ccc minimax
ccc openlux_opus
ccc openlux_sonnet
ccc openrouter_dsflash
ccc openrouter_dspro
ccc openrouter_glm_flash
ccc xiaomi_mimo_pro

---

## Profile JSON 字段速查

ccc 不校验字段,下面是常用参考(不是必填):

| 字段 | 用途 | 示例 |
|---|---|---|
| `env.ANTHROPIC_BASE_URL` | 供应商 API 地址 | `https://api.minimaxi.com/anthropic` |
| `env.ANTHROPIC_AUTH_TOKEN` | token | kdbx 源时永不落盘 |
| `env.ANTHROPIC_MODEL` | 默认模型 | `MiniMax-M3` |
| `permissions.defaultMode` | 权限模式 | `bypassPermissions` |
| `skipDangerousModePermissionPrompt` | 跳过危险模式确认 | `true` |

`mcpServers` / `hooks` 原样透传,ccc 不解析。

---

## 常见问题

**`ccc` 不被识别?**
ccc 不装、不改 PATH。用全路径 `./dst/ccc.exe`,或自己把 `dst/` 加进 PATH。

**`profile not found: xxx`?**
`ccc list` 看现有名字;确认 kdbx `cc` 组里有对应 entry。

**报 `credential file ... not found`?**
主密码还没存进 cred 文件:
```bash
./dst/ccc.exe cred store
```

**报 `kdbx: bad credentials`?**
kdbx 主密码改过了,重跑 `ccc cred store`。

**想清掉凭据重新开始?**
```bash
./dst/ccc.exe cred delete
./dst/ccc.exe cred store   # 重新输密码
```

**父进程的 `ANTHROPIC_AUTH_TOKEN` 没生效?**
这是故意的。ccc 会剥掉父进程所有 `ANTHROPIC_*`,保证用的是 profile 里的值,不会被嵌套污染。

**想看 ccc 实际跑什么命令?**
`ccc which <profile>`,带绝对路径,不实际启动。

---

## 想了解更多

- [docs/MIGRATION.md](docs/MIGRATION.md) — 从旧版 ccs 迁过来
- [docs/ccc验收标准.md](docs/ccc验收标准.md) — 完整验收矩阵
- [docs/go架构规范.md](docs/go架构规范.md) — 架构设计理由
- [docs/todolist.md](docs/todolist.md) — 阶段开发计划

---

MIT
