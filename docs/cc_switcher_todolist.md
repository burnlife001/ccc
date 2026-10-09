# cc_switcher / ccc — kdbx 免密同步（DPAPI 路径）开工清单

> 目标：`apikey.kdbx` 成为 apikey 唯一权威源；同步脚本**免交互输密码**从 kdbx 物化出 `settings_*.json`（cc_switcher）和 `~/.ccc/profiles/*.json`（ccc）。
> 路径：方案 A —— 主密码经 **DPAPI**（CurrentUser 范围）加密落盘，运行时解密后交给 pykeepass 开库。
> 安全边界（如实记录）：DPAPI 保护 = "ACL 保护的明文"同级——同机同用户进程均可解；价值在于文件被拷走/进云盘/进备份后不可解。运行时明文 JSON 不可避免（`claude --settings` 只认明文）。
> 硬约束：ccc 本体不碰 kdbx（守 `go架构规范.md` stdlib-only）；同步链路是独立脚本，依赖（pykeepass）只在 `.venv` 里。

## Phase 0 — 前置确认（动手前先答完）

- [ ] 确认 kdbx 解锁方式：纯密码 / 密码+keyfile（若是复合密钥，cred 引导流程要同时 DPAPI 化 keyfile 路径或密码）
- [ ] 确认 kdbx 内条目命名规范：建议 `ccc/<profile-name>`，约定字段（token 单独存 or 整条 settings JSON 存附件/备注）
- [ ] `uv venv` 建好 `.venv`（位置建议 `decrypt/cc_switcher/.venv`），`uv pip install pykeepass`
- [ ] 验证：`.venv` 内 `python -c "import pykeepass; print(pykeepass.__version__)"` 通过
- [ ] 验证：`Get-Command keepassxc-cli` 有无（仅作备用对照，不作主路径）

## Phase 1 — 凭据引导（一次性）

- [ ] `sync/kdbx_cred.ps1`：`Read-Host -AsSecureString` 读主密码 → DPAPI `ProtectedData::Protect(..., 'CurrentUser')` → 写 `~/.ccc/.kdbx.cred`（ACL 收紧到仅本人，脚本内 `icacls` 一步到位）
- [ ] 解密侧放在 Python 里做（`ctypes` 调 `CryptUnprotectData`），避免 PowerShell 中间明文字符串驻留
- [ ] 验证：另开一个 pwsh 窗口跑解密 → 成功；把 `.kdbx.cred` 拷到另一台机器（或另一用户）解密 → 必须失败

## Phase 2 — 同步脚本核心

- [ ] `sync/sync_profiles.py`（诊断输出内嵌，每步打印）：
  1. 读 `~/.ccc/.kdbx.cred` → DPAPI 解密（内存中，不落盘）
  2. pykeepass 开 `E:/__work/__frequently/personal_data/kdbx_cpq/apikey.kdbx`
  3. 按 `ccc/` 组遍历条目，模板（仓库内 `settings.template.json`，可进 git）+ token 拼装成完整 settings JSON
  4. **校验 JSON 合法性**后，临时文件 + 原子替换写入两处目标：
     - `~/.ccc/profiles/<name>.json`（ccc 用）
     - `decrypt/cc_switcher/settings_<name>.json`（旧脚本过渡期用）
  5. 输出每个 profile 的 SHA256 前 8 位（drift 检测基线）
- [ ] 失败即非零退出，绝不留半截文件（原子写保证）
- [ ] 验证：改 kdbx 里一个 token → 跑同步 → 两处文件哈希变化且内容正确；故意改坏一个条目 → 脚本报错且已有文件未被破坏

## Phase 3 — 双端接入

- [ ] `cc_switcher.ps1` 不改逻辑（仍读 `settings_*.json`），只在菜单顶部加一行"最后同步时间 / 与 kdbx 哈希是否一致"提示
- [ ] ccc 侧零改动——`profiles/*.json` 本来就是它的输入
- [ ] pwsh 入口 `sync.ps1` 薄封装：激活 venv → 调 py → 透传退出码
- [ ] 验证：同步后 `ccc which <profile>` 指向新文件；`./dst/ccc.exe <profile> -p "ping"` 冒烟通过

## Phase 4 — 迁移与收尾

- [ ] `decrypt/cc_switcher/settings_*.json` 现有值与 kdbx 对账（以 kdbx 为准，参考记忆：minimax 有效 key 以 decrypt 为准——对账时逐条人工确认哪边新）
- [ ] 对账完成后 decrypt 目录降级为**只读归档**，唯一写入口 = 同步脚本
- [ ] `.gitignore` 确认覆盖：`~/.ccc/profiles/`、`.kdbx.cred`、`settings_*.json`、`.venv/`
- [ ] readme 更新：同步脚本用法、DPAPI 安全边界一句话说明、key 轮换流程（改 kdbx → 跑 sync）
- [ ] 验证（端到端）：轮换一个 key → kdbx 改 → sync → ccc 实测调用成功 → 旧 key 确认失效

## 明确不做

- ccc Go 二进制内不实现任何 kdbx/DPAPI 逻辑
- 不做 keyfile 路径（方案 B），除非日后要加二要素
- 不做 Windows Hello 解锁（keepassxc-cli/pykeepass 均不支持）
- 不同步到 `~/.claude/` 下任何位置
