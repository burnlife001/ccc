# ccc — kdbx 工具链纯 Go 化(Phase 5)开工清单

## 背景

kdbx 项目已整体迁入本仓库 `kdbx/`。价值链主体早已在 Go 里
(`src/kdbx.go`:kdbx 解锁、compose/serialize 字节级复刻、DPAPI 读侧)。
本次收尾:**把仅剩的 ps1+Python 功能移植进 ccc,然后整目录退役脚本**。

明确不做(非目标):

- ❌ 远程推送到 192.168.1.123(`ccc_kdbx_123.ps1` 直接删除,不移植)
- ❌ merge-write `~/.claude/settings.json`(ccc 的非目标;`ccc <profile>` 已取代)
- ❌ 交互菜单 UI(`ccc list` + `ccc <profile>` 已取代)
- ❌ 保留任何 Python/uv/.venv 运行时依赖

## 任务分解

### T1 `ccc cred store` —— DPAPI 凭据写入端(唯一功能缺口)

对应 `kdbx/src/kdbx_cred.py` 的 `store()` + `prompt_password()`。

- 新增 `CryptProtectData` 封装(与 `kdbx.go` 现有 `cryptUnprotectData` 对称,
  同文件,user-scope,`dwFlags=0`)
- 交互式读密码:**不回显明文**(掩码 `*` 或关回显),支持退格,Ctrl+C 中止;
  **非交互终端(stdin 非 tty)直接报错退出**,防管道/后台卡死
  - 优先 stdlib 实现(kernel32 GetConsoleMode/SetConsoleMode 关回显 + 读行);
    若引入 `golang.org/x/term` 需在 commit message 说明理由
- 写文件前**先验密码能开 kdbx**(`verify_kdbx` 等价:用候选密码
  `gokeepasslib` 解码一次,失败则不落盘)
- 落盘:`base64(DPAPI blob)` → `<kdbx同目录>/.kdbx_master.cred`(沿用
  `credPathFor()`)

### T2 `ccc cred check` / `ccc cred delete`

对应 `kdbx_cred.py` 的 `check` / `delete` 模式。

- `check`:读 cred → DPAPI 解密 → 试开 kdbx。全部通过打 `CHECK_OK`;
  任一环节失败给明确原因(cred 缺失 / 解密失败-换用户或换机 / 密码已失效)
- `delete`:删 cred 文件;不存在时友好提示而非报错

### T3 cli 接线 + 错误提示改向

- `cli.go` Dispatch 增加 `cred store|check|delete` 子命令分发;
  `ccc cred`(无参数)打用法
- `kdbx.go` 现有错误提示中所有 `pwsh -File ...ccc_kdbx.ps1` 改为 `ccc cred store`
- `ccc list` 在 cred 缺失时的 warn 同步改指 `ccc cred store`

### T4 kdbx/ 目录终态

删除:`src/*.py`、`__pycache__/`、`.venv/`、`ccc_kdbx.ps1`、`ccc_kdbx_123.ps1`。
保留:`apikey.kdbx`(gitignored)、`apikey.kdbx.example`、`.kdbx_master.cred`
(gitignored)、`README.md`。

### T5 文档

- 重写 `kdbx/README.md`:KeePassXC 建库/改密说明保留,流程改为
  `ccc cred store` 初始化 + `ccc list` 验证;删掉 uv/python/.venv 前置
- `readme.md` 如提到 ps1/python 流程,同步更新

## 验收

按 `docs/ccc验收标准.md` 执行,P0 全过才准收工。

## 验证方式

```powershell
go build -ldflags "-s -w" -o dst/ccc.exe ./src
go test ./src/
# 人工端到端:删 cred → ccc list 报错指向 ccc cred store →
# ccc cred store 输密码 → ccc list 成功 → ccc cred check 打 CHECK_OK
```
