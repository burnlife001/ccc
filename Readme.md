## 适合谁：
- 希望apikey在公共机上保密
- 希望自由切换模型和apikey

---

## 前置环境: 
- windows, `claude code`
- clone/下载本仓库
- 按`kdbx\README.md`3步操作完毕

--- 

## 快速开始：
### 加密功能
- 运行scripts\ccc_manager.ps1,选[2. 安装注册]
- 任意目录空白处右键，选claude

### 子agent委派
ccc minimax -p "say hello"

生成全部可用 profile 的命令清单:scripts\ccc_manager.ps1 菜单 7 → `scripts\ccc_list.md`
(数据源 `ccc list --tsv`;kdbx profile 变动后重跑刷新)

---

## 进阶：加密/明文模式
### 一、加密模式：
- 1.切到无settings.json的模式：scripts\01.ccc_manager.ps1(toggle...粉碎settings.json)
- 2.设置当前右键菜单的claude模型：scripts\02.ccc_switch.ps1(通过ccc.exe读取加密数据注入claude)
- 3.右键claude

### 二、明文模式：
- 1.切到有settings.json的模式：scripts\01.ccc_manager.ps1(toggle...)
- 2.设置本地模型的settings.json：scripts\03.ccc_settings.ps1(通过settings.json工作,这是claude默认模式)
- 3.右键claude

## 公共机模式(公共机模式不能使用明文settings.json)
- **开启/关闭**:scripts\ccc_manager.ps1 菜单 4。`on` 静默免密;`off` 需主密码 —— kdbx 编辑源在场则解锁验密,缺席则与 `.cred` 已存主密码比对
- **离开公共机前**:菜单 5 删除本地凭据文件(dst/.cred,连带清旧格式残留)

---

## 凭据文件与命令(`ccc cred`)
运行时凭据只有一个文件:`dst\.cred` —— 一个 **user-scope DPAPI 密封**的 JSON 信封,
一次装下三样东西:kdbx 主密码、公共机闸位、厂商快照。别的 Windows 用户解不开 →
闸门 fail-closed(锁死)。详见 `docs\安全切换逻辑.md`。

| 命令 | 作用 |
|---|---|
| `ccc cred store` | 交互输 kdbx 主密码写入 `.cred`(只更新主密码,不动闸位与快照);在场解锁 kdbx 验密,缺席两次输入一致即存 |
| `ccc cred check` | 报告主密码在位 / `public` 值 / `providers` 条数;kdbx 校验缺席时降级为附注,不翻盘 |
| `ccc cred sync` | 从 kdbx 的 cc 组导出厂商快照进 `.cred`(公共机上也允许;需要 kdbx 编辑源在场) |
| `ccc cred delete` | **不可逆**:粉碎 `.cred` + 连带清旧格式文件(公共机收尾用) |

- 旧版本的两个文件(`.kdbx_master.cred` + `.public_machine.flag`)在**首次读取时自动迁移**
  到 `.cred`,迁移成功后旧文件删除;迁移失败则原样保留(绝不静默丢闸位状态)。
- kdbx 写命令(`price` / `add-models` / `rm-model` / `migrate`)成功后自动 resync 快照 ——
  日常 `ccc list` / `ccc <profile>` 命中快照,不必每次解锁 kdbx。

## 两种使用方法：
- 目录空白处右键，菜单中出现claude点击执行
- 终端执行： ccc minimax [-p] "say hello"

---

## settings 模板(settings.template.json)
settings.json 由模板渲染生成:模板提供 env / modelPicker / theme / hooks / permissions / effort 等全部静态内容,ccc 从 `.cred` 的 providers 快照注入动态三值(token、base_url、三档模型,模板里以 `${TOKEN}` `${BASE_URL}` `${MODEL}` `${HAIKU}` `${SONNET}` `${OPUS}` 占位);快照未命中时回退 kdbx 编辑源。

- 模板两份来源:`src\settings.template.json` 编译时嵌入 exe(默认);`<exeDir>\settings.template.json` 存在则优先(改模板不用重编译)
- 发布包内的 `dst\settings.template.json` 是随 exe 分发的可改副本(构建时由 `src\settings.template.json` 覆盖生成,始终同源);与 exe 同目录即自动生效
- 外部模板解析失败 → 直接报错(不静默回退内嵌)
- openrouter profile 渲染时删掉模板里的 `modelPicker` 键
- SHOWSETTINGS 写盘时:模板赢,盘上已有文件只补模板没有的键(嵌套对象递归合并,数组整体替换)
- 思考档位(effort)归模板顶层的 `"effort"` 键管(默认 `max`),`ccc effort` / `CCC_EFFORT` 已移除;单次临时覆盖照旧:`ccc minimax --effort low ...`(命令行透传)
- SHOWSETTINGS 态下请走右键菜单启动 claude(裸 `claude` 只读盘上 settings.json);若用 `ccc <profile>` 内联启动,盘上文件与内联 JSON 的 hooks 会叠加执行

---

## OpenRouter 低价模型菜单(scripts\openrouter_models.ps1)
| 菜单 | 作用 |
|---|---|
| 1 拉取模型清单 | 生成openrouter-models.md |
| 2 按 index 并入模型 | 输入.md清单里的编号(如 `3,7 12`),把对应模型并进 kdbx|
| 3 更新模型价格 | 按 OpenRouter 实时价格刷新库里已有模型的价格/上下文并重排 |
| 4 删除模型 | 把`.kdbx中cc/openrouter` 现有模型列成编号表,选一条删掉 |

公共机模式下菜单 2 / 3 / 4 被 exe 侧拒绝(exit 4);菜单 1 不受影响。

---

## agent命令示例：

ccc Kimi_k28 -p "写个前端"
ccc Kimi_k3 -p "写个后端"
ccc deepseek_flash -p "写个3D枪战游戏"
ccc deepseek_pro -p "编个三体续集"
ccc minimax -p "听说你把自己憋死了"
ccc openlux_opus -p "设计一个正向博弈社会框架"
ccc openlux_sonnet -p "把所有算法都写完"
ccc openrouter_dsflash -p "你是最便宜的模型吗"
ccc openrouter_dspro -p "听说你是anthropic家的？"
ccc openrouter_glm_flash -p "听说你能连续几天不休息？"
ccc openrouter_qwen38_27b -p "刚买的袜子为什么有个洞？"
ccc xiaomi_mimo_pro -p "解决黎曼猜想"

## 微软defender报毒
管理员执行添加可信目录：scripts/trust_gobuild_ccc(admin).ps1
