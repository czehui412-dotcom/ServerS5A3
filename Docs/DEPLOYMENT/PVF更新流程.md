# PVF 更新流程（format=2 整包架构）

> **历史副本（截至 1.0.31）：勿按本文件执行。** 权威版在 `f:/90cn/轻装上阵/deploy/PVF更新流程.md`；上线记录与回滚点见同目录《运维变更台账.md》。

> **本册管 PVF / EXE / npk / DLL 等客户端资源发布。** 源码与登录器本体更新 →《源码更新手册.md》；从零搭建 →《部署手册.md》；新架构设计 →《登录器架构重构设计.md》。环境常量（三副本路径、端口、compose）见《部署手册》§0。
>
> **当前线上（2026-11-22 起）**：PVF `f072c239`（129,680,712 B）｜EXE `2551b8a7`（112,828,416 B）｜launcher **1.0.31** / bundle `1.0.31`（sha `4260b636`）｜新壳 `PurpleMagic-1.0.31.exe` sha `0e90215c`（payload `79355003`）｜bootstrap **seq 163** / format=2 / game.strategy=archive｜整包 sha `88005960`（150,130,253 B，5 文件）｜服务端 `97afbf0a`（维护踢人）。

**自更新死锁坑（2026-11-22，已修）**：1.0.28~1.0.30 的 `UpdateLauncherCore` 把 bundle job 的 work 目录建在 `state-anticheat\tmp\bundle\<id>`，但 `BundleInstaller.ValidateJob`/`ReportHealthy` 要求 `install\.updates\<id>` → 自更新 Apply 必抛「更新事务目录无效」→ UI 收到"更新完成"自动关、helper 崩溃不重启 → 再开反复失败闪退。**死锁**：坏 core 无法把自己更新到修好的版本，必须**替换壳本体**（新壳内嵌修复版 payload）。1.0.31（commit `9082dcb`）已把 work 改到 `install\.updates\`。排查要点：看 `state-anticheat\tmp\bundle\` 下 job.json 全是 `phase:downloaded` 就是此坑；恢复=换 `client\PurpleMagic.exe` 为新壳，旧壳备份 `PurpleMagic.exe.bak-1.0.29`。

**维护模式（2026-11-22 新增）**：GM 系统设置页可手动开/关维护。开启后一次完成：① 服务端先落盘再踢全部在线玩家并拦截新登录；② 写 nginx edge 静态状态 `https://124.221.247.221/status/maintenance.json`；③ 登录器（1.0.30+）每 30s 经 core 用信任根校验读取状态，命中维护就 **kill 自己启动的 DNF.exe**（释放 `Script.pvf` 文件句柄）+ 显示维护横幅 + 禁用登录。换 PVF / 重启服务前先开维护，验收后关维护。**这是替换 Script.pvf 的前提——不关进程会 `Text file busy`。**详见《部署手册》维护模式章节。

**客户端目录规则（1.0.29）**：默认自动识别启动目录（登录器与 DNF.exe 同目录）；用户点「更改」手动选的目录持久化到 `identity.json` 的 `ClientDirectory`，**手动选择优先于自动识别**；所选目录必须含 `DNF.exe` + `Script.pvf`，否则拒绝。
>
> **玩家侧机制（新）**：登录器在线拉签名 bootstrap（format=2，只有一个 `Game` 契约）→ 对 Game 清单里每个文件算**最终 SHA-256 + 长度**→ 不一致才下载整包 → 包级 sha + 逐文件最终 sha 双重校验 → 事务提交。**没有 content/blob manifest、没有 make_content_manifest、没有 check/publish、没有 `content-check.txt`。**

---

## 0. 职责分工（2026-11-21 起）

| 责任方 | 负责什么 | 不碰什么 |
|---|---|---|
| **AI（助手）** | ① 服务端 PVF 发布：`release_pvf.py` 全 8 步（三副本 + 8 配置 + 6 索引 + 重建容器）<br>② 网关部署/重启/format=2 契约验证<br>③ 登录器 bundle 构建与发布（`upload_bundle.py`）<br>④ 信任根（profile 的 ExeSha/PvfSha）修改<br>⑤ 验收与回滚 | 不打包 zip、不上传网盘、不填直链 |
| **用户** | ① 用 **`DNF90资源更新工具-v3.exe`** 选目录打包（工具自动算 EXE/PVF 哈希并标出与网关是否一致）<br>② 把 zip 上传网盘，拿到 A/B 直链<br>③ 在工具里填 manifest + 直链 → 点「发布到网关」→ 点「验签 bootstrap」 | 不改服务端文件、不跑 `release_pvf.py`、不动网关容器 |

**交接点**：AI 完成 §4 服务端发布后，用户用工具打包时，工具的哈希提示必须显示 **「与网关一致 ✓」** 才继续；打包完的 zip 和 `.manifest.json` 都在用户所选目录里。

> 分工铁律：AI 发布游戏/网关，用户处理直连（网盘直链 + archive 发布）。任何一端发现契约不一致，先停在交接点核对，不要越过对方职责改文件。

---

## 1. 一次更新要动什么

| 类别 | 位置 | 漏了会怎样 |
|---|---|---|
| 副本① 游戏服 | `/srv/apps/ServerS5A3/runtime/docker/data/Script.pvf` | 游戏逻辑按旧 PVF 跑 |
| 副本② GM | `/srv/apps/PurpleMagic/runtime/selfhost/assets/Script.pvf`（10001:10001, 600） | GM 解析旧 PVF |
| 副本③ GM 挂载点 | `/srv/dnf-assets/Script.pvf`（**最易漏**） | GM 容器实际读这里 |
| 配置 ×8 | `service.json`、`gateway.json`×2、`indexer.json`、`prepared.json`、`private/player-profile.json`、`gm/queue.json`、`hub/state.json`(PvfSha256 **+ sequence+1**)、`compose.env`(PM_PVF_SHA256) | 校验失败 / 登录器按 equivocation 拒绝 / rebuild 后按旧哈希建索引 |
| 索引 ×6 | `runtime/selfhost/indexes/`：`pvf.bin`、`items.json`、`item-details-v3-<sha>.bin`、`quests-v1-<sha>.bin`、`professions-v1-<sha>.bin`、`expert-jobs-v1-<sha>.json` | `pvf.bin` 旧 → GM 崩溃循环 |
| Game 契约 | `hub/state.json` 的 `published.game`（`Delivery.URLs` / `PackageSha256` / `Files[].sha256,length`），由 archive 发布 API 生成 | 客户端下载错误文件或长度校验失败 |
| 登录器 | **换资源不需要动**；仅登录器代码改动才走《源码更新手册》§7 | — |

四个关键事实：① PVF 解密密钥是服务端内置常量，换文件即可解析；② 索引文件名带 PVF 校验和，旧校验和文件是死数据必须删；③ `pvf.bin` 不匹配 → GM **崩溃循环**（不是警告）；④ **手工改 `hub/state.json` 前必须 `docker stop purplemagic-gateway-1`**（运行中网关任何管理 API 请求都会把内存旧状态写回磁盘覆盖迁移）。

---

## 2. 多买多送三区块保护（每次 PVF 更新前后必做 ⚠）

服务端商城逻辑依赖 `etc/chn_cerashop_multibonus.etc` 的三个区块：`[private festival periods]`/`[private festival commodities]`/`[private festival rewards]`（大小写敏感）。丢失 → catalog fail-closed → 全商城报「物品栏空间不足」。

```powershell
$pvf = "F:\90cn\轻装上阵\deploy\tools\PVF-CLI\dist\pvf.exe"
& $pvf read --pvf "<本次 PVF 完整路径>" --path "etc/chn_cerashop_multibonus.etc" --format tokens --out "$env:TEMP\mb_check.json"
@('private festival periods','private festival commodities','private festival rewards') |
  ForEach-Object { "$_ : $((Get-Content $env:TEMP\mb_check.json -Raw) -match [regex]::Escape('['+$_+']'))" }   # 三个都要 True
```
被上游 PVF/operator 覆盖丢了：从上一版好 PVF 提取重插（`数据迁移\_extracted\build_festival_plan.py`，token 规则：保留 token 用 `payload_u32`、新增字符串/标签用 `text`），重插后跑《源码更新手册》§2 冒烟确认服务端兼容。**别用整文件覆盖旧 PVF 的方式回退**（会连带丢其他定制）。

**当前 201801 档位（官方循环，`b038d4c2` 起，2026-11-20 恢复；更新时务必保住）**：periods 行 201801 的 `RepeatFrom/RepeatItem/RepeatCount` = `25/0/0`（24 套循环标记）；`[private festival rewards]` 里 201801 只有 **22 档**：at=2..12 + at=14..24（跳过 13）。服务端 patch `3e4a0fe0` 识别 `RepeatFrom=25` 后，第 N≥25 套映射回 `(at-1)%24+1` 重发模板档位。

> **⚠ 血泪教训：不要用「12 套循环铺到 at=10000」的改法**（曾产出 `d7a6b3a2`，2026-11-20 回退）。服务端下发 packet `0x7f02` 每期固定 256 字节、**最多 24 档**（`len(tiers)>24` → 静默 `status=5` 空帧，不打日志），客户端也校验 `>24` 拒收。后果：游戏内「礼包多买多送」界面**空白**。协议只支持单一奖励循环（RepeatFrom），做不到 11 种奖励交替循环。
>
> **✅ 现行做法（seq131 上线）**：PVF 写 `RepeatFrom=25 / RepeatItem=0 / RepeatCount=0`，档位 ≤24 个模板；服务端 patch 负责循环重放。改周期只需改 PVF `RepeatFrom`（=周期+1），服务端无需再动。详见版本台账 seq131。

---

## 3. AI 操作：服务端 PVF 发布

> 本节全由 AI 执行，用户不参与。新 PVF 由用户放到约定目录（如 `F:\90cn\轻装上阵\轻装更新\<日期>\Script.pvf`）。

```powershell
cd F:\90cn\轻装上阵
python deploy\tools\PVF-CLI\release_pvf.py --pvf "F:\90cn\轻装上阵\轻装更新\<日期>\Script.pvf" --from 1 --to 8
```

| 步 | 内容 | 耗时 |
|---|---|---|
| 1-4 | 算 SHA256 + 改 `gmindex\gmconfig.json` → `--build-pvf-index` → `--build-item-details` → `--build-quest-index` | ~50s |
| 5-6 | 上传 PVF+4 索引(~345M) → 三副本 + 8 配置 + sequence+1 + 索引改名/删旧/chown | 视带宽 |
| 7-8 | `manage.py up`(python3.12) + SERVER_IP post-fix + 重建 server → 重启 GM/网关 + 验收 | 3~4min |

全程约 **4.5~5 分钟**，末尾出现 `完成，用时 … | PVF <sha前16>` 且 §6 验收全绿即完成。

```powershell
# 断点续跑 / 单步重做（索引已建好时）
python deploy\tools\PVF-CLI\release_pvf.py --from 5
python deploy\tools\PVF-CLI\release_pvf.py --only 7
```

> `gmindex` 运行时目录在官方克隆侧（`deploy\PurpleMagic\gmindex`，gitignored），脚本 `GMIDX` 常量依赖此路径，勿移动。`pvf.bin` 已存在时先删；`--build-item-details` 的 `reused=true` 不重建 items.json，丢失时删 `item-details-v3-<sha>.bin` 触发全量重建。
>
> **旧架构的 Length 盲区已消除**：旧版 apply 只换哈希不换 Length，需停网关手工改 `state.json`。新架构下 File 的 length 由用户打包工具从真实文件读取写进 manifest，随 archive 发布 API 一起写入 `Game.files[]`，**不需要手工改 state.json**。
>
> **旧架构的 manifest/blob 入位步骤已删除**：不再有 `make_content_manifest.py` / `publish_content.py` / `verify_bootstrap.py` / `content-blobs/` / `releases/channels/stable/manifest.signed.json`。这些是废弃通道，不要再执行。

---

## 4. 用户操作：打包 + 直链发布

工具：**`deploy\tools\release\dist\DNF90资源更新工具-v3.exe`**（源码 `resource_updater.py`，打包脚本 `build_resource_updater.bat`，说明见同目录《资源更新工具说明.md》）。旧 `.exe` / `-v2.exe` 有缺陷，勿用。

### 4.1 打包
1. 打开工具 → 「① 打包本地资源」→ 「浏览」选 **新 PVF 所在目录**（如 `轻装更新\20261121`）。
2. 工具会**自动**做三件事：
   - 计算该目录 `DNF.exe` / `Script.pvf` 的 sha256，填入「② 同步信任根」两个输入框；
   - 在输入框右侧显示 **「与网关一致 ✓」** 或 **「与网关不一致（网关 xxxx…）」**；
   - 把输出 zip 和 manifest 默认设为该目录下的 `game-archive.zip` / `game-archive.zip.manifest.json`。
3. **确认两行都显示「与网关一致 ✓」** 才继续。若红字不一致，说明服务端还未发布新 PVF（AI 还没做 §3），或选错目录。
4. 文件清单默认 5 项（`DNF.exe`/`Script.pvf`/`dstr.dat`/`CombatPower.dll`/`DpsMeter.dll`）；换 npk/DLL 时按相对路径加行（如 `ImagePacks2/xxx.npk`）。白名单外的路径工具会拒绝。
5. 点「仅打包」→ 得到 zip + manifest（都在所选目录）。

### 4.2 网盘上传（人工，工具不代做）
把 zip 上传到网盘，拿到稳定的 A（主）/ B（备）直链。**这是整条链唯一的信任缺口**：工具只信签名里的 `PackageSha256`，不校验直链实际内容。上传后建议用浏览器打开直链，确认下载到的是刚打的包（可对比文件大小 150,130,253 B）。

### 4.3 发布到网关
1. 「③ 发布到网关」→ 资源清单选刚生成的 `game-archive.zip.manifest.json`（选目录时已自动填好）。
2. 填直链 A，必要时填 B（只填 A 也可以，v3 已修复空参数 bug）。
3. 点「发布到网关」→ 日志出现 `PUT 200 ... ok:true, sequence N` 即成功。
4. 点「验签 bootstrap」→ 确认：
   - `format: 2`、`launcher: 1.0.29`、`game.strategy: archive`；
   - `legacy content/archive: False False`；
   - `profile.PvfSha256` 与 `Game.files[Script.pvf].sha256` 一致，`ExeSha256` 与 `DNF.exe` 一致；
   - sequence 比发布前 +1。

> **防重复发布**：每次成功 PUT 都会 sequence+1，即使包内容相同。发错包或重复点击只能再发一次正确包把 sequence 推上去（sequence 只增不减），不要试图回滚序号。

---

## 5. 变更类型路径表

| 变更 | AI 做 | 用户做 |
|---|---|---|
| **PVF 换版** | §3 `release_pvf.py` 全 8 步 + 三区块/201801 检查 + 验收 | §4 打包（选新 PVF 目录）→ 上传网盘 → 发布 → 验签 |
| **EXE 换版** | 停网关 → 双改 profile.ExeSha256 + SecondClient 白名单 → seq+1 → 起网关（《源码更新手册》§8） | 选含新 EXE 的目录打包 → 上传 → 发布 → 验签 |
| **npk / DLL（不换 PVF/EXE）** | 无需动 | 选目录打包（清单加 npk/DLL）→ 上传 → 发布 → 验签 |
| **登录器代码改动** | 构建 + bundle POST（《源码更新手册》§7） | 无需动（玩家自更新登录器） |
| **反作弊策略** | 两处 profile 的 AntiCheat 段（停网关）→ seq+1 | 无需动 |
| **网关代码改动** | 上传源码 → `docker compose build gateway` → force-recreate → 验签 | 无需动 |

---

## 6. 验收清单（新架构）

AI 完成 §3 后核对：
- [ ] 三副本 `sha256sum` 全 = `<NEW_SHA>`；`indexes/` 无旧 SHA 残留、属主 10001
- [ ] bootstrap：sequence +1、PvfSha256/ExeSha256 新值、launcher 版本不变、`format: 2`
- [ ] `purplemagic-gm-1` Up（非 Restarting），日志 `external GM service ready` 无 `mismatch`
- [ ] 游戏服 healthy、0 `level=ERROR|panic`、`dnfbridge started` 时间新于发布、`game_ports=120`
- [ ] GM 面板能搜到新 PVF 新增道具，名称正确

用户完成 §4 后核对（工具「验签 bootstrap」）：
- [ ] `format: 2`、`legacy content/archive: False False`
- [ ] `profile.PvfSha256 == Game.files[Script.pvf].sha256`、`profile.ExeSha256 == Game.files[DNF.exe].sha256`
- [ ] `Game.files[Script.pvf].length == 新 PVF 真实字节数`（129,680,712）
- [ ] `Delivery.strategy: archive`、`packageSha256` = 本次 manifest 的 sha、sequence 已 +1
- [ ] 浏览器打开直链下载到的包大小/sha 与 manifest 一致
- [ ] 游戏内验证本次内容；多买多送三区块仍在（§2）

---

## 7. 常见坑

| 坑 | 解法 |
|---|---|
| GM 崩溃循环 `PVF disk index header/source mismatch` | `pvf.bin` 旧；本机 `--build-pvf-index` 重传，先 `docker stop purplemagic-gm-1` 止血 |
| GM 崩溃循环 `item index cache does not match the loaded PVF` | `items.json` 的 `pvfChecksum` 仍是旧 PVF：本地已有 `item-details-v3-<新sha>.bin` 使 `--build-item-details` 走 `reused=true` 跳过重建（**回退到旧 PVF 时必踩**）。修：删该 bin → 重跑 `--build-item-details` → 重传 `items.json`+bin 入位 → `up -d --force-recreate gm`。发布后 `head -c 200 indexes/items.json` 核对 `pvfChecksum` |
| 服务器 python 版本 | manage.py / prepare.py 一律 `python3.12`（默认 3.9 不支持 `write_text(newline=)`） |
| up 后 SERVER_IP 变内网 / db 3307 丢 | 《源码更新手册》§4 ④⑤ post-fix；`--force-recreate` |
| compose 项目名错 / restart 不重读 env | 游戏服 `-p servers5a3-local`；网关 `-p purplemagic --env-file runtime/selfhost/compose.env -f deploy/selfhost/compose.yaml` |
| 「假成功」：文件全绿但进程持旧 PVF | 比对容器 `StartedAt` 与日志 `dnfbridge started` 时间 |
| `/srv/dnf-assets` 最易漏 | GM 容器挂这里，不是 `selfhost/assets` |
| sequence 没 +1 / 倒退 | 客户端按 equivocation 拒绝；只增不减 |
| 属主错 | PurpleMagic 侧 10001:10001（assets 600、pvf.bin/items.json 644、其余 600） |
| SSH 多层引号炸 | 复杂 python 写脚本 scp 上去执行，或 base64 管道；**不要用 PowerShell 内联多行 python** |
| 索引积压 | 两侧按 SHA 积历史 `item-details/quests`（各 218M）；定期只留当前 SHA + pvf.bin/items.json |
| 客户端弹 InIT | PVF 与定制 DNF.exe 不配套；换 PVF 来源前**先本地双击 DNF.exe 验证能进游戏**再发布 |
| Windows subprocess GBK 崩 | Python 调 gmtool/ssh/pvf.exe 用 bytes + `decode(errors='replace')`，别用 `text=True` |
| 全服卡「请先完成客户端资源更新」 | 新架构下 = profile 与 Game.Files 的 EXE/PVF 哈希不一致；用工具「验签 bootstrap」对账，不要回退 format=2 |
| 永久「正在检查更新」 | 旧 core 1.0.25/1.0.26 的状态机 bug（已最新分支漏发 ready）；已由 1.0.28 修复，勿降级 |
| 直链指向旧包 | 工具只信签名 sha 不校验直链内容；玩家下载后逐文件校验失败。上传后务必浏览器打开直链核对 |
| 重复发布 | 每次 PUT 都 seq+1；发现发错只能再发正确包，不要手工改 sequence |
| **GM 面板大面积 503**（活动/深渊/注册限制等"接口暂不可用"） | **游戏服容器缺 `DNFBRIDGE_PLAYER_ADMIN_SECRET`** → `/api/game/admin` 鉴权失败 404 → 网关 Runtime.Call 全挂。根因：`instance.json` 丢了 `player_admin_secret` 字段（`manage.py up` 从该字段生成 env）。修：把该字段补回（值=网关 `gateway.json` 的 `management.gameToken`），再按 §3 步骤 7 重建（`manage.py up --host-ip 10.0.0.14` + SERVER_IP post-fix + force-recreate）。**每次 PVF 发布后核对**：`docker exec servers5a3-local-server-1 sh -c "tr '\0' '\n' </proc/1/environ | grep PLAYER_ADMIN_SECRET"` 必须有值 |

---

## 附录 A：PVF 内容补丁（改档后发布）

工具 `deploy\tools\PVF-CLI\dist\pvf.exe`（`info|list|read|search|export|patch|verify`）。流程：**审计 → plan JSON → patch → 同一审计复核 → 冒烟 → §3 发布**；patch 产物 `output_sha256` 即 `<NEW_SHA>`。

```powershell
cd deploy\_local\ServerS5A3\go-server
go run ./cmd/scratch/giftboxaudit -pvf <pvf> -out ..\tools\PVF-CLI\audit.json
cd ..\tools\PVF-CLI
python make_giftbox_plan.py && dist\pvf.exe patch --pvf <pvf> --plan plan_giftbox.json --out candidate.pvf
$env:DNFBRIDGE_REAL_PVF_SMOKE="<candidate.pvf>"; cd ..\..\ServerS5A3\go-server
go test -run TestRealPVFSeriaShopPricingSmoke -v -count=1 ./internal/services/dnfbridge
```
要点：整批修（礼盒 `[expiration date]` 必须 `2028-12-31 06:00:00`）；锚点只能是「标签后第一个值 token 之后」；旧 token 保留 `type+payload_u32`（0 不能被 omitempty 掉）、新增字符串只给 `type+text`；定位用 `pvf read --format tokens` 的 token 顺序；标签大小写先用 `pvf search` 在原版 PVF 确认；服务端拒绝原因看容器内 `logs/packet_log.txt`。

---

## 附录 B：版本台账（摘要）

> `sequence` 是 hub 发布序号（每次发布 +1），与「第 N 次更新」无关。2026-11-21 seq157 起进入 format=2 整包架构；此前的 content/blob 通道记录仅作历史参考，**不要再用旧工具复现**。完整历史在 git `patches` 提交、服务器 `.bak-*` 备份与 `数据迁移\_extracted` 产物清单中。

| 日期 | PVF | EXE | 说明 |
|---|---|---|---|
| 2026-11-17 ~ 11-20 (seq 70~148) | 多次 | 多次 | 旧 content/blob 通道时期：manifest 1.2.0→1.2.27、三副本+索引+check/publish。含多买多送循环方案迭代（已废弃 9166 档）、15 个 `[seeking] 3037 1` 任务改动、活跃度奖励调整、EXE 去水印 v1~v4、FE_TAIL 文案等。详见 git 历史与旧版手册备份 |
| **2026-11-21 (seq 157)** | `4e4020fa` | `2551b8a7` | **登录器架构重构 P0-P5 上线**：format=2 单 Game 契约、删 Content/Archive/format=1；launcher 1.0.28 / bundle `1.0.28`；bootstrap 旧字段清除。网关 seq156→157，新壳 sha `a5b798de` |
| **2026-11-21 (seq 158)** | `f072c239`(129,680,712) | `2551b8a7` | **PVF 更新（AI §3）**：`release_pvf.py --from 1 --to 8`（4.7 分钟），三副本一致、8 配置+6 索引、seq 157→158、GM ready/游戏服 0 ERROR/game_ports=120。新 Game 契约自动同步 PVF 哈希 |
| **2026-11-21 (seq 159~161)** | `f072c239`（未变） | `2551b8a7`（未变） | **新资源工具发布整包（用户 §4）**：manifest 5 文件、`packageSha 88005960`(150,130,253B)，发布时修正 PVF length 129680703→129680712。用户重复点击导致 seq 159→160→161（无内容变化）。v2 工具因空 fallback 参数 bug 曾提交失败，v3 已修复并增加目录哈希自动读取 + 网关一致性提示 |
| **2026-11-21（登录器 1.0.29，seq 未变）** | 未变 | 未变 | **客户端目录修复**：`identity.json` + `Preferences` 加 `ClientDirectory`，「更改」按钮持久化并优先于自动识别；bundle sha `2d0f968e…`、新壳 `7f92d8fa…` 已替换 `client\PurpleMagic.exe`。**同日 GM 面板大面积 503**：`instance.json` 缺 `player_admin_secret` → 游戏服 admin 鉴权 404 → 网关 Runtime.Call 全挂；补回后活动/深渊/注册限制/账号均恢复 200（见 §7） |

---

## 附录 C：旧「铺 9166 档循环」方案（**已废弃，勿执行**）

曾尝试把 1-24 套奖励原样、26 套起重发并每 12 套跳过一套循环到 at=10000。产物 `d7a6b3a2` 于 2026-11-20 回退，原因：服务端 packet `0x7f02` 最多 24 档，9166 档导致多买多送界面空白；协议只支持单一奖励循环（RepeatFrom）。**现行循环走服务端 patch `3e4a0fe0` + PVF `RepeatFrom=25 / RepeatItem=0 / RepeatCount=0` + 22 档模板（见 §2），不要再用任何铺档方案。**
