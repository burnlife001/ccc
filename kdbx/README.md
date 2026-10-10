# kdbx — ccc 的配置存储

`ccc` 从单个 KeePass `.kdbx` 文件读取所有供应商配置(token、base_url、模型名),
运行时纯内存合成 settings JSON,token 永不落盘。

本目录只放那个 kdbx 文件 + 本说明,其它工具链已整体迁入 `ccc cred` 子命令,
**不再需要 Python / PowerShell / uv / .venv**。

---

## 文件清单

| 文件 | 是否入 git | 说明 |
|---|---|---|
| `apikey.kdbx.example` | ✅ | 空模板库,不含真实 token。复制为 `apikey.kdbx` 后填你自己的 key。 |
| `apikey.kdbx` | ❌ (.gitignore) | 你的真实库。文件名可改,路径在 `dst/.env` 的 `CCC_KDBX_PATH` 里指。 |
| `.kdbx_master.cred` | ❌ (.gitignore) | DPAPI user-scope 加密的主密码。换用户/换机器需重输。 |

---

## 首次使用(3 步)

### 1. 建库

用 [KeePassXC](https://github.com/keepassxreboot/keepassxc/releases) 打开
`apikey.kdbx.example`,另存为 `apikey.kdbx`:

- **数据库密码**(主密码):随便起一个,记住就行,后面 `ccc cred store` 会要
- **数据库凭据**:`KeepassXC → 数据库 → 数据库凭据 → 更改密码`
- 删掉示例 entry,自己按需新建

### 2. 录入供应商

每条 entry = 一个供应商。**title** 随便起,**password** 填这个供应商的 API token,
**Notes** 写一段 JSON:

```json
{
  "base_url": "https://api.minimaxi.com/anthropic",
  "profiles": {
    "minimax_m3":           { "model": "MiniMax-M3" },
    "minimax_m3_highspeed": { "model": "MiniMax-M3-highspeed" }
  }
}
```

- `base_url`:供应商 API 地址
- `profiles`:key 是 `ccc <name>` 要敲的名字,value 里 `model` 会被展开成
  `ANTHROPIC_MODEL` env。可选 `haiku`/`opus`/`sonnet` 覆盖同族模型。

把这些 entry 放进名为 **`cc`** 的组(可改,通过 `CCC_KDBX_GROUP` 环境变量指)。

### 3. 存主密码 + 验证

```bash
# 告诉 ccc kdbx 在哪(相对路径相对 dst/ccc.exe 所在目录解析)
echo 'CCC_KDBX_PATH=..\kdbx\apikey.kdbx' >> dst/.env

# 交互式输入主密码 → 试开 → DPAPI 加密落盘
./dst/ccc.exe cred store

# 验证:list 能列出 profile 即成功
./dst/ccc.exe list
```

`ccc cred store` 会:

1. 提示 `Enter kdbx master password:`(无回显,每键打 `*`)
2. 用候选密码试开 kdbx,**失败不落盘**(避免覆盖一个对的 cred)
3. DPAPI(user-scope)加密 → base64 → 写 `<kdbx 同目录>/.kdbx_master.cred`
4. 打 `VERIFY_OK` + `STORED: ...`

之后 `ccc <profile>` 启动时自动读这个 cred,不需要重复输密码。

---

## 日常命令

| 命令 | 用途 |
|---|---|
| `ccc cred store` | 首次 / 主密码改过后重新存 |
| `ccc cred check` | 检查 cred 还能解 + 能开 kdbx(打 `CHECK_OK`) |
| `ccc cred delete` | 删 cred(下次启动会报错指向 `ccc cred store`) |
| `ccc list` | 列出所有 profile |

---

## 安全模型

- **DPAPI scope = 当前用户 + 当前机器**。换 Windows 账号 / 把 kdbx 拷到另一台机,
  原 `.kdbx_master.cred` 解不开 —— 重跑 `ccc cred store` 即可。
- **token 在磁盘上的存活时间 ≈ claude 一次运行的时间**。
  `ccc <profile>` 启动时合成 settings JSON → 写 `%TEMP%/ccc-settings-*.json` → 启
  claude → 退出即删临时文件,cred 文件只存加密的主密码,**不存任何 token**。
- **不写 `~/.claude/`、不注入 MCP/hook、不改 `CLAUDE_CONFIG_DIR`、不起守护进程**。

---

## 常见问题

- **改了 kdbx 主密码**:删 `.kdbx_master.cred` 重跑 `ccc cred store`。
- **换了用户/机器**:同上,DPAPI 绑定当前用户 + 当前机器。
- **`ccc list` 报 `credential file ... not found`**:跑 `ccc cred store`。
- **`kdbx: bad credentials`**:cred 是旧的,kdbx 主密码已被改 → 重跑 `ccc cred store`。
- **管道里跑 `ccc cred store`**:会立即报 `requires interactive terminal`,不卡死。