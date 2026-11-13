## 适合谁

- 希望 apikey 在公共机上保密
- 希望自由切换模型和 apikey

---

## 前置环境

- Windows + PowerShell 7 + `claude code`
- 克隆/下载本仓库
- 按 `kdbx/README.md` 完成 3 步准备

---

## 快速开始

### 加密入口

1. 运行 `scripts/ccc_manager.ps1`，选「2. 安装注册」
2. 按提示输入主密码
3. 在任意目录空白处右键，选择 claude

### 子 agent 委派

```
ccc minimax -p "say hello"
```

生成可用 profile 清单：运行 `scripts/ccc_manager.ps1`，选菜单 7 或菜单 8，结果写入 `scripts/ccc_list.md`。

- 私人机用菜单 7
- 公共机用菜单 8
- 凭据库有改动后，重跑菜单 8 刷新清单

---

## 加密 / 明文模式

### 加密模式（推荐）

1. `scripts/01.ccc_manager.ps1` → 切换到无 settings.json 的模式
2. `scripts/02.ccc_switch.ps1` → 选择 claude 使用的模型
3. 右键 claude

### 明文模式

1. `scripts/01.ccc_manager.ps1` → 切换到有 settings.json 的模式
2. `scripts/03.ccc_settings.ps1` → 写入模型配置
3. 右键 claude

公共机模式下不能使用明文模式。

---

## 公共机模式

- **开启**：`scripts/ccc_manager.ps1` 菜单 4，选 `on`
- **关闭**：菜单 4，选 `off`，按提示验证
- **离开公共机前**：菜单 5 删除本机凭据，菜单 6 删除 apikey.kdbx（不可恢复）

---

## 常用场景

| 场景 | 操作 |
|---|---|
| 首次使用 / 换新机器 | 菜单 2 安装注册，按提示输入主密码 |
| 日常使用 | 右键 claude |
| 切换模型 | 菜单 3 切换右键入口（明文仅私人机可用） |
| 机器借出前 | 菜单 4 选 `on` |
| 机器收回 | 菜单 4 选 `off`，按提示验证 |
| 修改了凭据库内容 | 菜单 2 重新安装注册 |
| 忘记主密码 | 菜单 2 重新安装注册并重设主密码 |
| 凭据文件异常 | 执行 `ccc cred delete` 后重新安装注册 |
| 离开公共机 | 菜单 5 删除本机凭据 + 菜单 6 删除 apikey.kdbx |
| 公共机上使用明文模式 | 会被拒绝，先关闭公共机模式 |

---

## 两种使用方法

- 目录空白处右键，点击菜单中的 claude
- 终端执行：`ccc minimax [-p] "say hello"`

---

## OpenRouter 低价模型菜单（`scripts/openrouter_models.ps1`）

| 菜单 | 作用 |
|---|---|
| 1 拉取模型清单 | 生成 `openrouter-models.md` |
| 2 按编号并入模型 | 输入清单编号（如 `3,7 12`） |
| 3 更新模型价格 | 按 OpenRouter 实时价格刷新已有模型 |
| 4 删除模型 | 列出现有模型，按编号删除 |

公共机模式下菜单 2 / 3 / 4 不可用。

---

## 命令示例

```
ccc Kimi_k28 -p "写个前端"
ccc Kimi_k3 -p "写个后端"
ccc deepseek_flash -p "写个3D枪战游戏"
ccc deepseek_pro -p "编个三体续集"
ccc minimax -p "虽然你有点疯，但能干活"
ccc openlux_opus -p "设计一个正向博弈社会框架"
ccc openlux_sonnet -p "把所有算法都写完"
ccc openrouter_ling-3.0-flash -p "262K上下文的模型能干点啥？"
ccc openrouter_qwen3.7-flash -p "你是最便宜的1M模型吗？"
ccc openrouter_glm-5.3-flash -p "听说你能连续几天不休息？"
ccc xiaomi_mimo_pro -p "解决黎曼猜想"
```

---

## 杀毒软件提示

本工具会处理敏感信息，安全软件出现提示属正常现象。需要放行时，管理员运行：

```
pwsh scripts/win-defender-trust/trust_gobuild_ccc_admin.ps1
```

## .kdbx安全科普：
.kdbx属于安全敏感资源，注意字典暴力破解，不要让你的密码进入破解字典
最佳实践：用你能记住的的数字(比如生日)，与字母组合，不低于10位
