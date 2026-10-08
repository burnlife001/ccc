# 迁移指南:从旧版 ccs 到 ccc

> 适用对象:已经在用 TypeScript 版 `ccs`(`E:/__work/__github/ccs/`)的用户
> 预计耗时:5 分钟

---

## 核心结论

**Profile JSON 字节级兼容** —— 你现有的 `~/.ccs/settings_*.json` 文件**不需要改任何字段**就能直接给 ccc 用。

ccc 与旧版 ccs 的唯一差别是**怎么找到这些文件**:
- 旧版:`~/.ccs/settings_<name>.json`
- 新版:`~/.ccc/profiles/<name>.json`

下面三步把数据迁过去,旧版 ccs 不需要卸载,两边可以共存。

---

## 三步迁移

### 第 1 步:把 profile 文件搬过去

`~/.ccs/` 下的每一个 `settings_<name>.json` 对应 ccc 的 `<name>.json`,只把 `settings_` 前缀去掉:

```bash
# 创建目标目录
mkdir -p ~/.ccc/profiles

# 批量复制并重命名(只对 settings_*.json 生效)
for f in ~/.ccs/settings_*.json; do
  name=$(basename "$f" .json | sed 's/^settings_//')
  cp "$f" ~/.ccc/profiles/"$name.json"
done

# 看一眼结果
ls ~/.ccc/profiles/
# 应该看到 minimax_m3.json  openrouter_xxx.json  ...
```

旧版 ccs 的文件可能是符号链接(指向 `E:/__work/__frequently/personal_data/apikey/decrypt/cc_switcher/`),`cp` 默认会展开成实文件——这是好事,token 落盘到 `~/.ccc/` 下不依赖加密 vault。

> ⚠️ **Windows 提示**
> git-bash 没有 `~/.ccc/` 别名,请用绝对路径:
> ```bash
> mkdir -p "/c/Users/$USER/.ccc/profiles"
> cp /c/Users/$USER/.ccs/settings_*.json "/c/Users/$USER/.ccc/profiles/"
> ```
> 或者 pwsh:
> ```powershell
> New-Item -ItemType Directory -Force "$env:USERPROFILE\.ccc\profiles"
> Copy-Item "$env:USERPROFILE\.ccs\settings_*.json" "$env:USERPROFILE\.ccc\profiles\"
> Get-ChildItem "$env:USERPROFILE\.ccc\profiles"
> ```
> ⚠️ 注意 Copy-Item 默认会**复制符号链接目标的内容**(而不是链接本身),与 git-bash 一致。

### 第 2 步:装 ccc

```bash
git clone https://github.com/burnlife001/ccc.git
cd ccc
./build.ps1      # 自动编译并安装到 D:\Programs\ccc\
ccc version      # 应该看到 ccc x.y.z
```

详见 [readme.md](../readme.md#安装)。

### 第 3 步:验证

```bash
# 等价于旧的: ccs minimax_m3 -p "OK?"
ccc minimax_m3 -p "OK?"

# 续接会话:旧的 ccs minimax_m3:continue -p "..."
ccc minimax_m3:continue -p "..."
```

期望:
- 输出 `OK`(纯文本,非 stream-json 乱码)
- exit code 0
- `~/.ccc/sessions.json` 自动生成,存了 `minimax_m3` 的 session_id

---

## 命令对照表

| 旧版 ccs | ccc | 行为差异 |
|---|---|---|
| `ccs <name>` | `ccc <name>` | 完全一致 |
| `ccs <name> -p "任务"` | `ccc <name> -p "任务"` | 完全一致;stream-json 自动剥成纯文本 |
| `ccs <name>:continue -p "..."` | `ccc <name>:continue -p "..."` | 完全一致 |
| `ccs list` | `ccc list` | 完全一致 |
| `ccs add <name>` | `ccc add <name>` | 模板字段名/层级一致 |
| `ccs rm <name>` | `ccc rm <name>` | 完全一致 |
| `ccs which <name>` | `ccc which <name>` | 完全一致 |
| `ccs config` | ❌ 无 | ccc 不需要(无配置目录,只有 profile 文件) |
| `ccs doctor` | ❌ 无 | 出问题直接看 `ccc which <name>` 拼出来的命令 |
| `ccs proxy` | ❌ 无 | ccc 不做 OpenAI 兼容代理 |
| `ccs --target droid <name>` | ❌ 无 | ccc 只透传给 claude,不做多 target |

ccc 是**最小透传器**,不做的事比旧版多很多,这是刻意的。详见 [readme.md](../readme.md#为什么用-ccc) 的"不做的事"清单。

---

## 数据迁移详细表

### `~/.ccs/` 下的文件

| 旧路径 | 动作 | 新路径 |
|---|---|---|
| `~/.ccs/settings_<name>.json` | 复制并去掉 `settings_` 前缀 | `~/.ccc/profiles/<name>.json` |
| `~/.ccs/config.yaml` | 不迁 | ccc 没有等价的全局配置 |
| `~/.ccs/delegation-sessions.json` | 不迁 | 由 ccc 重新生成 `~/.ccc/sessions.json` |
| `~/.ccs/backups/` | 不迁 | 旧版的备份机制 |
| `~/.ccs/cache/` `logs/` `hooks/` `mcp/` `prompts/` `shared/` | 不迁 | ccc 不维护这些运行时数据 |

### `~/.ccc/` 初始化后的目录

```
~/.ccc/
├── profiles/                # 你 cp 过来的所有 json
│   ├── minimax_m3.json
│   ├── openrouter_dsv4flash.json
│   └── ...
└── sessions.json             # 首次 ccc <name> -p "..." 后自动生成
```

`profiles/` 是手工维护的,`sessions.json` 是 ccc 自己写的(只在 `:continue` 用到)。

---

## 回滚

ccc 与旧版 ccs **不冲突**(路径、PATH、profile 文件都分开),想完全切回旧版:

```bash
# 1. 删 PATH 里的 D:\Programs\ccc(系统环境变量里手动改)
# 2. 删 D:\Programs\ccc\ccc.exe
# 3. ~/.ccc/ 留不留都行,不影响旧 ccs
```

`~/.ccc/profiles/` 留着也可以——它们就是合法的 settings json,旧 ccs 也可以读(只是路径不同)。

---

## 常见迁移问题

### Q: cp 过去后 `ccc <name>` 还是 `profile not found`?

- 文件名大小写不对?`ccc list` 看一下能找到啥
- 路径写错?Windows 上 `~/.ccc/profiles/` 对应 `C:\Users\<你>\.ccc\profiles\`,不是 `D:\...`

### Q: 复制后 token 没了?

`~/.ccs/settings_minimax_m3.json` 是 symlink,目标文件在加密 vault(`E:/__work/__frequently/personal_data/apikey/decrypt/cc_switcher/`)。`cp` 应该展开了内容,但某些 shell 默认走的是软链(保留链接本身)——确认 `~/.ccc/profiles/minimax_m3.json` 文件大小 > 0、不是软链:

```bash
ls -la ~/.ccc/profiles/minimax_m3.json
# 不是 -> 开头说明展开了
```

### Q: 旧 ccs 的某些 flag 在 ccc 里透传报错?

ccc 走"未知 flag 原样追加"的策略,Claude Code 自己的 flag 全部透传。如果旧 ccs 有**自己的**特殊 flag(如 `--target`),ccc 不识别,但它会原样拼到命令行里,然后 Claude Code 不认识就报错——这是符合预期的(ccc 不该识别旧 ccs 的私有 flag)。

### Q: 想两套并存过渡期怎么搞?

最简单:两个都装,profile 文件各管各的。

```bash
# 旧版继续用 ~/.ccs/settings_*.json
ccs minimax_m3 -p "..."

# 新版用 ~/.ccc/profiles/*.json
ccc minimax_m3 -p "..."
```

两边**互不干扰**(路径分离、PATH 里两个 exe 同名但优先级由 PATH 顺序决定)。建议把 `D:\Programs\ccc\` 放在旧版 ccs 的目录**前面**,这样默认走新版。

---

## 迁移后第一个任务清单

- [ ] `ccc version` 输出版本号
- [ ] `ccc list` 看到搬过来的所有 profile
- [ ] `ccc <name>` 进交互式会话,`/status` 显示对的 `ANTHROPIC_BASE_URL`
- [ ] `ccc <name> -p "回复 OK"` 输出 `OK`,exit code 0
- [ ] `ccc <name>:continue -p "你记得我吗"` 能续上
- [ ] `~/.ccc/sessions.json` 已被创建

全绿 = 迁移完成。