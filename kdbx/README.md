## 首次使用(4 步)

- 安装[KeePassXC](https://github.com/keepassxreboot/keepassxc/releases)
- 使用[KeePassXC]打开`apikey.kdbx`,为每个模型输入apikey(不用的模型删了)
- 改密码(默认123,`CTRL+SHIFT+,`/安全/数据库凭据/更改密码)

---

## 示例库(`apikey.kdbx.example`)的主密码怎么给

`apikey.kdbx.example` 是随仓库发布的**公开示例库**,主密码默认 `123`
(公开信息 —— 公开仓库里就摆着它)。用它跑 `ccc kdbx sync-notes` 时,主密码
**只经环境变量传**,不进命令行:

```powershell
$env:CCC_KDBX_PW = "123"
ccc kdbx sync-notes .\kdbx\apikey.kdbx.example
Remove-Item Env:CCC_KDBX_PW     # 用完清掉
```

- `ccc kdbx sync-notes --password xxx` 已被**移除并报错拒绝** —— 明文密码进
  命令行会进进程列表 / shell 历史 / CI 日志(2026-10-27)。
- 不设 `CCC_KDBX_PW` 时,命令会**交互输入**(不回显,不打印 `*`)。
- 发布脚本 `sync-to-github-release.ps1` 只在调用示例库的那一次调用期间设置
  该环境变量,调用结束立刻还原。

## 真库(`apikey.kdbx`)的主密码

真库的主密码**从不**进命令行、也不进环境变量 —— 它由 `ccc cred store`
交互输入,存进 `<exeDir>/.cred` 信封(user-scope DPAPI 密封 + 固定 entropy +
仅当前用户 + SYSTEM 可读的 ACL)。信封里**不存 apikey**:厂商 token 每次
现取自本库。信封被解开 ≠ 拿到厂商密钥。
