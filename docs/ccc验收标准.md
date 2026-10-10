# ccc 验收标准 —— Phase 5:kdbx 工具链纯 Go 化

每条验收须记录:操作 → 预期 → 实际证据。P0 全过才准收工;P1 是质量门槛。

## P0 —— 必须全过

| # | 操作 | 预期 |
|---|---|---|
| A1 | 删除 `.kdbx_master.cred` 后 `ccc list` | 报错/警告指向 `ccc cred store`,**不再出现任何 ps1 字样** |
| A2 | `ccc cred store`,交互输入**正确**主密码 | 回显掩码(无明文);提示 VERIFY_OK;生成 `.kdbx_master.cred`;随后 `ccc list` 正常列出 profile |
| A3 | `ccc cred store`,输入**错误**密码 | 明确报"密码无法解锁 kdbx";**不落盘** cred 文件 |
| A4 | `ccc cred check`(cred 正常) | 输出 `CHECK_OK`,退出码 0 |
| A5 | `ccc cred check`(cred 文件被篡改成合法 base64 但解不开/解出错误密码) | 明确失败原因,退出码非 0,不 panic |
| A6 | `ccc cred delete` 后文件不存在;再执行一次 | 第一次删除成功;第二次友好提示 NOT_FOUND,退出码 0 |
| A7 | `echo "" \| ccc cred store`(stdin 非 tty) | 明确报"需要交互终端"并退出,**不卡死、不等待输入** |
| A8 | `go test ./src/` + `go build -ldflags "-s -w" -o dst/ccc.exe ./src` | 测试全过(含新增 cred 用例);构建出 exe |
| A9 | `ls kdbx/` 与 `ls kdbx/src`(后者应不存在) | 终态仅:`apikey.kdbx`、`apikey.kdbx.example`、`.kdbx_master.cred`、`README.md`;无 `.py`/`.ps1`/`.venv`/`__pycache__` |
| A10 | `git grep -ni "python\|\.ps1\|venv" -- kdbx/ src/ readme.md` | 除 README 中"已退役"说明性文字外,无存活引用 |

## P1 —— 质量门槛

| # | 检查 | 预期 |
|---|---|---|
| B1 | 新增依赖审计 | 优先 stdlib;若引入 `golang.org/x/term` 等,commit message 写明理由 |
| B2 | 密码驻留内存最小化 | 验证完成后立即清引用(沿用 `openAndUnlockKdbx` 的 `defer 清空` 模式) |
| B3 | cred 文件内容 | 严格 `base64(DPAPI ciphertext)`,与旧 Python 版生成的文件**互通**(旧 cred 能被 Go 读、Go 写的 cred 逻辑上等价) |
| B4 | 错误前缀 | cred 相关错误带 `kdbx` 或 `cred` 前缀,便于识别;经 `%w` 包装 |
| B5 | `ccc cred`(无子命令)/ 非法子命令 | 打用法说明,退出码非 0 |

## 非目标(再确认)

- 不做 192.168.1.123 远程推送
- 不写 `~/.claude/settings.json`
- 不做交互菜单
- 不在 `~/.claude/` 下写任何文件
