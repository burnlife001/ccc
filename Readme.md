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
- 1.切到无settings.json的模式：scripts\ccc_manager.ps1(toggle...粉碎settings.json)
- 2.设置当前右键菜单的claude模型：scripts\ccc_switch.ps1(通过ccc.exe读取加密数据注入claude)
- 3.右键claude

### 二、明文模式：
- 1.切到有settings.json的模式：scripts\ccc_manager.ps1(toggle...)
- 2.设置本地模型的settings.json：scripts\ccc_settings_local.ps1(通过settings.json工作,这是claude默认模式)
- 3.右键claude

## 公共机模式(公共机模式不能使用明文settings.json)
- **开启/关闭**:scripts\ccc_manager.ps1 菜单 4。需要 kdbx 主密码(本机已存凭据则免输)
- **离开公共机前**:菜单 5 删除本地凭据文件(dst/*.cred)

## 两种使用方法：
- 目录空白处右键，菜单中出现claude点击执行
- 终端执行： ccc minimax [-p] "say hello"

---

## 思考档位(effort)
档位写在 `dst\.env` 的 `CCC_EFFORT`，ccc 启动 claude 时自动带上 `--effort <档位>`。

| 档位 | 含义 |
|---|---|
| low / medium / high / max | 对应 claude 的 `--effort` 四档 |
| auto(默认) | 不传 `--effort`，由 claude 自己决定 |

- 查看当前档位：`ccc effort`
- 设置档位：`ccc effort high`（直接改写 `dst\.env`）
- 单次临时覆盖：`ccc minimax --effort low ...`（命令行优先于 .env）

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

## 发版(打 tag)

根目录三个 `sync*.ps1`(`sync-to-github.ps1` / `sync-to-github-release.ps1` / `sync-to-ccc-switch.ps1`)的公共实现都放在 `scripts-common\sync-common.ps1`,各脚本 dot-source 一份:

| 共用函数 | 作用 |
|---|---|
| `Test-GitRepo` | 目录是不是 git 仓库 |
| `Get-RepoCommitMessage` | 读仓库 HEAD 的 commit message |
| `Sync-ExampleKdbx` | 用 `apikey.kdbx.example` 覆盖镜像侧的 `apikey.kdbx` |
| `Invoke-RoboCopy` | robocopy 增量拷贝(统一旗标,返回 exit code) |
| `Invoke-CommitAndPush` | status → add → commit → push |
| `Sync-NotesToExample` | 构建 `ccc-syncnotes` 并把真库 notes 灌进示例库 |
| `Get-RemoteTag` | `ls-remote` 查远端是否已有某 tag、指向哪个 commit(顺带判断网络可达) |
| `Get-PushFailureKind` | 推送报错归类:`exists`(远端已存在)/ `network`(网络错误)/ `other` |
| `Invoke-Tag` | 源仓库 + 镜像仓库各打一个同名 tag,再一起推送 |

打 tag 的入口:`sync-to-github.ps1` 菜单 3、`sync-to-github-release.ps1` 菜单 4。

一次打两个:**源仓库(ccc-private,gitee)与 GitHub 镜像(ccc-github)各打一个同名 tag,再一起推上各自的 origin**。

| 场景 | 命令 |
|---|---|
| 同步代码到 GitHub 镜像(整仓) | `.\sync-to-github.ps1` 菜单 2 |
| 打 tag + 一起推送(整仓脚本,交互输入版本号) | `.\sync-to-github.ps1` 菜单 3 |
| 打 tag + 一起推送(整仓脚本,非交互) | `.\sync-to-github.ps1 -Tag v1.0` |
| 打 tag + 一起推送(发布脚本,非交互) | `.\sync-to-github-release.ps1 -Tag v1.0` |
| 只打 tag 不推送 | `.\sync-to-github.ps1 -Tag v1.0 -NoPush` |

前置条件(脚本自检,任一不过 → 零改动,不留下半截 tag):

- 两侧已跟踪文件无未提交改动(tag 必须打在已提交的状态上);未跟踪文件只提示、不阻塞(不进 commit,tag 打不到)
- GitHub 镜像已同步到源仓库当前 commit —— 否则打出去的 tag 指向的代码和源不是同一份(脚本按 HEAD commit message 比对)
- 版本号形如 `v1.0` / `v1.0.1`(CI 只监听 `v*`)

tag 已存在不再是硬错误,脚本按远端现状分色提示:

| 情况 | 提示 |
|---|---|
| 远端已有该 tag(同一 commit) | 绿色「已存在,无需重复推送」 |
| 远端已有该 tag(指向别的 commit) | 绿色「之前已打过,跳过推送」+ 黄色注明两边指向的 commit(远端不动,不覆盖已发布版本) |
| 只有本地有该 tag | 绿色「本地已有,直接推送」 |
| 连不上远端(网络错误) | 红色「网络错误」+ 可重试的 `git push origin <tag>` 命令 |

失败语义:镜像侧打 tag 失败 → 自动回滚源仓库已打的 tag;推送失败不回滚 tag(远端可能已收到),只提示可重试的 `git push origin <tag>` 命令。

注意两个脚本的镜像仓库形态不同,发版效果也不同:

| 脚本 | 镜像内容 | 推 tag 到 GitHub 的效果 |
|---|---|---|
| `sync-to-github.ps1` | 整仓(含 `src\`、`.github\workflows\`) | **触发 `release.yml` 编译发版** |
| `sync-to-github-release.ps1` | 只有发布物白名单(exe / kdbx 示例 / scripts / Readme) | 只是版本标记,不触发工作流(工作流不在该 commit 里) |

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
