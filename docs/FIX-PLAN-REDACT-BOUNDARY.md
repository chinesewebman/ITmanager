# M30：`redact` 规则 1/2/3 的值边界与相互咬合（G-34 + G-35 同一轮）

> 2026-09-12 立项。**状态：需求文档 rev2（三路对抗审查已过，处置见 §7）。**
> 上游：TODO **G-34**（规则 2/3 的值边界残余）、**G-35**（规则 1 的输出被规则 3 二次误伤）；
> 交接前置见 `docs/FIX-PLAN-HARDENING.md` §8.6（M28/F 的实测边界表，结论是
> 「下轮开工前先把上表 5 组固化成 P1 用例」）。
>
> 归属说明：用户口中的「M28 通知链路保真」里，G-33（台账陈旧）与 G-38（包级 map 竞态）已结案，
> G-39（告警规则的渠道选择不生效）要先定「告警命中哪条规则」的产品语义、G-37（OpenAPI 补全）
> 是文档完整度 —— 本条是其中唯一**依赖已锁定、可独立闭环**的脱敏缺陷对。
>
> **为什么两项必须同一轮**（M28/F 的结论，本轮复核后仍成立）：G-35 的修法要求「规则 3 知道
> 自己在 URL 里」，而这个状态一旦引入，G-34 的「漏」用例期望值会同时移动（§2.3 的方案表）。

## 0. 规模决策（含流程缩放）

- **只改 `redact.Text` 三条规则的**值边界**与**组合方式**：不动键词表、不引入新依赖、不换状态机（§6）。
- **先固化边界用例、再改代码**：这是 M28/F 的交接前置，作为实现的第一步（§5.1）。
- 纯函数改动，**无迁移、无 schema、无前端影响**；爆炸半径集中在「所有日志/落库出口的文本形态」。

**流程缩放决策**：与 M29 同口径 —— 需求与可执行细节**合并为一份文档**（本文件已含精确
before/after 正则、具名用例、逐条验收值，拆成两份只会产生两份会漂移的期望值），审查走
**一轮三视角**（安全 / 正确性 / 一致性），每条发现**先由我复现再处置**（§7）。
中止条件：出现「泄漏 vs 丢信息」之外的设计取舍、或门禁红 —— 本轮均未触发（F4 是审查新增的
泄漏闭合项，按本文件既有的价值排序处置，代价已实测，见 §2.4 / R5）。

**覆盖门槛**：`redact` 是被全仓日志出口依赖的安全函数，属核心逻辑 → 语句覆盖 ≥80%（Go 无分支
覆盖工具，沿用 M28 的可度量口径：语句覆盖 + 变异反证）。

## 1. 实测（当前 `Text()` 的真实输出，探针实测、非推演）

### 1.1 漏（真凭据明文留下）

| # | 输入 | 当前输出 | 归属 | 本轮 |
|---|---|---|---|---|
| L1 | `Authorization: Bearer "SECRET"` | **原样（整条明文）** | 规则 2 | 修（F1） |
| L2 | `authorization: bearer "SECRET"` | **原样** | 规则 2 | 修（F1） |
| L3 | `Authorization: Bearer  "SECRET"`（两个空格） | **原样** | 规则 2 | 修（F1） |
| L4 | `Authorization: Bearer<TAB>"SECRET"` | **原样** | 规则 2 | 修（F1） |
| L5 | `Authorization: Bearer "SECRET`（未闭合） | **原样** | 规则 2 | 修（F1） |
| L6 | `Authorization: Bearer 'SECRET` | **原样** | 规则 2 | 修（F1） |
| L7 | `password=&SECRET` | **原样** | 规则 3 | 修（F2） |
| L8 | `password=;SECRET` | **原样** | 规则 3 | 修（F2） |
| L9 | `password=,SECRET` | **原样** | 规则 3（**台账未列**） | 修（F2） |
| L10 | `password=""abc` | **原样** | 规则 3（**台账未列**） | 修（F2） |
| L11 | `password= '"SECRET'` | **原样** | 规则 3（**台账未列**） | 修（F2） |
| L12 | `password：SECRET`（全角冒号） | **原样** | 规则 3（**审查新增**） | 修（F4） |
| L13 | `{"password":["SECRET"]}` | `{"password":***"SECRET"]}` | 规则 3（**审查新增**） | **登记 G-46** |

**L1–L4 是本次最重要的修正**：M28/F 的边界表只记了「未闭合的 `Bearer "SECRET`」，实测
**闭合的 `Authorization: Bearer "SECRET"` 同样整条泄漏**（同一处值类 `[^\s"']+` 要求首字符非引号，
闭合与否无关）。而从配置文件/文档里复制粘贴出来的引号凭据**通常是闭合的** —— 所以 L1 的真实
可达性**高于**台账里那条。大小写（L2）与多空白/Tab（L3/L4）也是同一处值类的不同表现，一处修全好。

**L12 的现实性**：中文 IME 打冒号默认输出 `：`，中文语境里手敲的 `token：xxx` 是常态而非巧合。

**L13 为什么只登记不修**：真正的缺口不是「数组」而是**嵌套值**——`{"password":{"a":"SECRET"}}`
同样泄漏（实测），闭合 `[`/`{` 需要嵌套匹配，属 §6「不换状态机」的范围。只补 `\[[^\]]*\]`
会引入一条**新的分支**，同时把既有 `password=[SECRET]x` 的覆盖面收窄（现在整段被吞，加了
数组分支只吞 `[SECRET]`）—— 这正是 R4 类风险，收益不抵。登记为 TODO **G-46**，用例钉住当前输出。

### 1.2 过度脱敏（不泄漏，但丢信息）—— G-35 的全谱

规则 1 先把 URL 塌缩成 `scheme://host`，规则 3 再把 `host:port` 当键值形态、把**端口**当值抹掉。
实测受影响的 host（都是「按 `-`/`_` 分段后恰好等于敏感词」）：

| 输入 | 当前输出 | 端口 |
|---|---|---|
| `http://token:8080/x` | `http://token:***` | 丢 |
| `http://secret:8080` | `http://secret:***` | 丢 |
| `http://my-token:8080/x` | `http://my-token:***` | 丢 |
| `redis://pwd:6379/0` | `redis://pwd:***` | 丢 |
| `http://access_token:8080/x` | `http://access_token:***` | 丢 |
| `https://pwd:443/a?token=abc` | `https://pwd:***` | 丢 |
| `http://sign:8080/x`、`http://apikey:8080/x` | `…:***` | 丢 |
| `http://host:8080/x` | `http://host:8080` | 保留（对照） |
| `http://tokens:8080/x`、`http://passwords:8080/x`、`http://signed:8080/x` | 保留 | 对照 |
| `http://secret`（无端口） | `http://secret` | 对照 |

**上面的对照行原本的解释是错的**（一致性审查 #4 复现）：命中与否不取决于「敏感词后跟 `-`/`_` 或
**串结束**」——`http://secret` 不脱敏，`http://secret:8080` 脱敏，可见规则 3 的键名后**必须**有
`:`/`=`（分隔符）才可能命中；`tokens`/`passwords`/`signed` 保住的真实原因是敏感词后跟的是裸字母。

**丢的不只是端口**：命中后 `host:port` 整段变 `host:***`，而规则 1 的语义是「保留 host 供定位」——
端口没了，排障时无法区分同一主机的多个服务（8080/8081/…），正是规则 1 想保住的那点信息。

### 1.3 规则 1 的塌缩本身是安全的 —— **但只对 path/query/userinfo 成立**

`http://user:pass@host/x` → `http://host`；`http://token:8080/x?password=SECRET` → `http://token:8080`
（修后）—— path / query / userinfo 由**塌缩直接丢弃**，URL 内部**不需要**规则 3 参与。

**但 host 段是保留的**，所以「URL 内部不需要规则 3」这个前提**只对 path/query/userinfo 成立**。
反例（安全审查 #1 提出，我已复现）：`http://access_token=SECRET` 的 **host 本身就是 `key=value` 形态**，
现状被规则 3 兜住（`http://access_token=***`），分段后会**明文直出**。这是 F3 的**必要条件**
——详见 §2.3 的 F3b。

### 1.4 边界锁定（修 G-35 时不许改坏的既有行为，共 8 组）

| 输入 | 当前输出 | 期望 |
|---|---|---|
| `token="S` | `token="***` | 保持（rev1 曾误判为漏） |
| `password="SECRET"` | `password="***"` | 保持（**引号形态必须回写**，见 §2.2） |
| `{"password":"ab}c"}` | `{"password":"***"}` | 保持（审计 P1 口径：`}` `]` 不收窄） |
| `X-Api-Key: SECRET` / `api-key=SECRET` | `…: ***` / `…=***` | 保持 |
| `password=ab}c` / `password=[SECRET]` / `password=(SECRET)` / `password=[SECRET]x` | `password=***` | 保持 |
| `password=SECRET&x=1` | `password=***&x=1` | 保持（值尾的 `&` 不吃） |
| `Authorization: Bearer ""` | 原样 | **接受**（空凭据无内容可漏，§6） |
| `//token:8080/x` | `//token:***` | **接受**（规则 1 的 scheme-relative 分支只认带点域名/localhost/IPv6，见 §6） |

## 2. 修法

### 2.1 F1：规则 2 的值类补「任意个起始引号」

```
before  (?i)(\bauthorization\s*:\s*(?:bearer|basic)\s+)[^\s"']+
after   (?i)(\bauthorization\s*:\s*(?:bearer|basic)\s+)["']*([^\s"']+)
```

一条改动覆盖 L1–L6，并顺带覆盖 `Authorization: Bearer ""SECRET`（实测 `Bearer ***`）。
用 `*` 而不是 rev1 的 `?`：**上界不是安全边界，没有理由定在 1**（正确性审查 #3）。
闭合引号的**尾部**保留（`Bearer ***"`）—— 不追求把引号一起吃掉的「好看」，因为吃尾引号要
再引入一个分支，而收益只是观感。

### 2.2 F2：规则 3 的值类补「起引号」与「起分隔符」

```
before  (["']?)([^\s"'&,;]+)
after   (["']*)(?:[&,;][^\s]*|[^\s"'&,;]+)
```

- **起引号**（`["']*`，**必须是捕获组并回写** `${1}${2}***`）：修 L10 `password=""abc` →
  `password=""***`、L11 `password= '"SECRET'` → `password= '"***'`（**rev1 在这里把期望值写成了
  `password= '***'`，实测是前者** —— 正确性审查 #1）、L11 的三引号变体 `password='''SECRET`
  → `password='''***`（rev1 的 `{0,2}` 上界会漏，改用 `*` 后闭合）。
  > **实现注意（我原型踩到过）**：引号组一旦写成非捕获，§1.4 的三条锁定会全部破
  > （实测 `password="SECRET"` → `password=***"`、`token="S` → `token=***`、
  > `{"password":"ab}c"}` → `{"password":***"}`）。回写是锁定的前提，§5.2 有专门用例。
- **起分隔符**（`[&,;][^\s]*`）：修 L7–L9，**吞到下一个空白**。
  - 为什么不是「只吞分隔符后的一个 token」：`password=&next=SECRET` 若只吞 `next`，输出会变成
    `password=&***=SECRET` —— **吃掉了下一个键名、却把它的值留成明文**，那是**新引入的泄漏面**。
    吞到空白则得到 `password=***`（安全，§3.1 实测）。
  - 代价（**行为突变**，须告知）：`?a=1&password=&b=2` → `?a=1&password=***`。
    价值排序与 `redact.URL()` 的 `invalidURL` 注释一致：**宁可丢信息，也不回显原串**。

### 2.3 F3：规则 3 不再看见 URL —— 用「分段」，不用「回看」（G-35）

**为什么还需要 F3**：F1/F2 之后，规则 3 的值类边界**仍然**会把 `scheme://host:port` 的端口吃掉 ——
那与值类无关，是**规则 1 的输出又被喂给了规则 3**。

| 方案 | 做法 | 结论 |
|---|---|---|
| **A 回看** | 规则 3 改 `ReplaceAllStringFunc`，匹配起点紧跟在 `://` 之后则跳过 | **否决 —— 引入新泄漏面**。`://access_token=SECRET`：规则 1 不匹配（scheme-relative 分支要求带点域名/localhost/IPv6），而规则 3 的匹配恰好紧跟在 `://` 之后 → 被跳过 → 输出 `://access_token=SECRET` **明文**（现状是 `://access_token=***`）。安全性取决于攻击者可控文本的形状 → 不可接受。**实测见 §3.3** |
| **B 分段**（选定） | `urlRe.FindAllStringIndex` 把文本切成「URL 段 / 非 URL 段」交替；**URL 段只过 `URL()`**，非 URL 段才过规则 2/3 | 规则 3 **永远看不到 URL 文本**。「知道自己在 URL 里」这件事由**结构性分段**保证，不靠模式匹配去猜 |
| C 状态机 | 用小型状态机替掉三条正则 | 见 §6：等于重写 G-28 刚收敛的规则，收益不抵风险 |

**F3b（F3 的必要条件，不可拆）**：分段的正确性依赖一条不变式 —— **`URL()` 的输出不含凭据形状**。
现状只保证了「不含 path/query/userinfo」，**host 段里的 `=` 是缺口**：`http://access_token=SECRET`
的 host 就是 `access_token=SECRET`，`URL()` 原样返回。所以 F3 必须同时把 `URL()` 的塌缩口径
扩一条（与既有的 `strings.HasSuffix(u.Host, ":")` → `invalidURL` 完全同构的一行）：

```
+ if strings.Contains(u.Host, "=") { return invalidURL }   // host 里的 `=`：不是任何真实主机的形态，
                                                          // 却是「key=value 被 URL 吞掉」的典型形态
```

实测：`http://access_token=SECRET` → `<invalid-url>`、`http://access_token=SECRET:8080/x` →
`<invalid-url>`、`redis://access_token=SECRET/0` → `<invalid-url>`；而不带 `=` 的
`http://token:8080/x` → `http://token:8080`（G-35 修好）。**没有 F3b 的 F3 是净回归**（§3.2 / R4）。

**分段的实现要点**：`Text` 从「三次 `ReplaceAll` 顺序执行」变为「按 URL 匹配切段 → 段内跑规则
2/3 → 拼接」。URL 段用 `URL(m)` 的输出直接拼接，**不再**送回规则 2/3（这是 F3 的全部要点，
写反了就退回旧行为，§5.2 有专门变异钉住）。

### 2.4 F4：规则 3 的分隔符类补全角冒号（审查新增，超出 G-34/G-35 原始范围）

```
before  ["']?\s*[:=]\s*
after   ["']?\s*[:=：]\s*      （：= U+FF1A）
```

- **为什么纳入**：L12 是**真凭据明文泄漏**，触发条件与 §2.2/§2.3 同类（都是「不修就继续漏」），
  修法是一个字符、**只增加匹配、不改变任何既有命中**（§3.1 实测），符合本文件既有的价值排序。
- **代价（第二种行为突变，实测）**：中文没有词间空格，所以 `重置 password：请联系管理员` →
  `重置 password：***`（整句被吞）。判断：写英文键名 + 全角冒号的概率远低于真的在写凭据，
  且与 R3 的「吞到空白」是同一类代价、同一套价值排序。**若认为不可接受，回退只需删这 1 个字符**
  （用例与台账相应回滚，不影响 F1–F3）。

### 2.5 Where（变更清单）

| 文件 | 改动 |
|---|---|
| `backend/internal/redact/redact.go` | `authHeaderRe`（F1）、`kvSecretRe`（F2 + F4）、`URL()` 增 `=` 判据（F3b）、`Text()` 改分段（F3）；三段注释同步 |
| `backend/internal/redact/redact_test.go` | §5.1 的修复项用例 + §5.2 的三条守门用例 |
| `docs/TRAPS.md` | 新增 **T-56**（§5.3） |
| `TODO.md` | G-34 / G-35 结案；新增 **G-46**（L13 结构化值） |
| `CHANGELOG.md`、`08-部署运维.md` §8.4.3（**仓库根**） | §5.3 |

### 2.6 明确不改的边界（防止「顺手扩大范围」）

`urlRe` / `authHeaderRe` / `kvSecretRe` 的**键词表**与**URL 形状**定义一律不动（除 F4 的 1 个字符）；
`URL()` 除 F3b 外不动；`Text` 的导出签名与调用点不动（全仓 0 个调用点需要改）。

## 3. 候选实现的实测（原型已跑，用于给实现步骤定验收值）

### 3.1 修复项逐条（`≠` 表示与现状不同）

| 输入 | 现状 | 候选实现 |
|---|---|---|
| `password=&SECRET` / `;SECRET` / `,SECRET` | 原样 | `password=***` |
| `password=""abc` | 原样 | `password=""***` |
| `` password= '"SECRET' `` | 原样 | `` password= '"***' `` |
| `password='''SECRET` | 原样 | `password='''***` |
| `password=&` | `password=&` | `password=***`（空值也遮，无泄漏） |
| `password=&next=SECRET` | 原样 | `password=***`（**不**产生 `=SECRET` 明文） |
| `?a=1&password=&b=2` | 原样 | `?a=1&password=***`（**行为突变**） |
| `Authorization: Bearer "SECRET"` | 原样 | `Authorization: Bearer ***"` |
| `authorization: bearer "SECRET"` / 双空格 / Tab / 未闭合 / 单引号 | 原样 | `Authorization: Bearer ***`（尾引号形态随输入） |
| `Authorization: Bearer ""SECRET` | 原样 | `Authorization: Bearer ***` |
| `password：SECRET` / `token：SECRET` | 原样 | `password：***` / `token：***`（F4） |
| `http://token:8080/x` | `http://token:***` | `http://token:8080` |
| `http://secret:8080` / `http://my-token:8080/x` / `redis://pwd:6379/0` / `http://access_token:8080/x` / `https://pwd:443/a?token=abc` | `…:***` | `…:<port>` |
| `http://token:8080/x?password=SECRET` | `http://token:***` | `http://token:8080` |
| `http://token:8080/x password=abc&y=1` | `http://token:*** password=***&y=1` | `http://token:8080 password=***&y=1` |
| `中文前缀 http://token:8080/x 中文后缀 password=SECRET` | `中文前缀 http://token:*** …` | `中文前缀 http://token:8080 …`（UTF-8 段边界正确） |
| `http://access_token=SECRET` | `http://access_token=***` | `<invalid-url>`（F3b，**不加就明文**，见 §3.2） |
| `Get "http://access_token=SECRET": dial tcp: timeout` | `Get "http://access_token=***": …` | `Get "<invalid-url>": …`（F3b） |

**回归（候选实现与现状逐字符一致，必须保持，实测 27 组）**：§1.4 全 8 组 →
`token="***`、`password="***"`、`{"password":"***"}`、`…: ***`、`password=***`、
`password=***&x=1`、原样、`//token:***`；另加 `Authorization: Bearer ""`（原样）、
`Authorization: Basic dXNlcjpwYXNz`（`***`）、`Authorization: Bearer &SECRET`（`***`）、
`?a=1&access_token=X`（`***`）、`http://host:8080/x`、`http://tokens:8080/x`、
`http://user:pass@host/x`（→ `http://host`）、`://access_token=SECRET`（**分段下仍是
`://access_token=***`**）、`foo:://password=SECRET`（`***`）、`x=://access_token=SECRET`（`***`）、
`password=[SECRET]x`（`***`）。

### 3.2 被否决方案 A 与「无 F3b 的 F3」的实测（否决依据）

| 输入 | 现状 | 方案 A（回看） | F3 但**无** F3b |
|---|---|---|---|
| `://access_token=SECRET` | `://access_token=***` | **明文** | `://access_token=***` |
| `x=://access_token=SECRET` | `x=://access_token=***` | **明文** | `x=://access_token=***` |
| `foo:://password=SECRET` | `foo:://password=***` | **明文** | `foo:://password=***` |
| `http://token:8080/x` | `http://token:***` | `http://token:8080` | `http://token:8080` |
| `http://access_token=SECRET` | `http://access_token=***` | `http://access_token=***` | **`http://access_token=SECRET`（明文）** |
| `redis://access_token=SECRET/0` | `redis://access_token=***` | `redis://access_token=***` | **明文** |

即：方案 A 与「无 F3b 的 F3」**各自制造了一族新的明文泄漏** —— 前者 3 条（`://` 前缀路径），
后者 4 条（host 含 `=`）。这是选定「B + F3b」的决定性依据。

## 4. Risk

高风险改动（重写一个被全仓日志出口依赖的安全函数），故给 5 条具体失败模式：

- **R1｜分段实现写反 → 静默退回旧行为，且无测试能发现。** 若把 `URL()` 的输出再送回规则 2/3
  （或忘记跳过 URL 段），端口又会消失、而 §3.1 的「修复项」用例会红 —— 但若**只**有「修复项」
  用例，写反的实现同样会让它们红，所以真正的风险是「实现对了、但有人后来把分段改回顺序执行」。
  **缓解**：一条**专用**守门用例（`TestText_URL段不参与规则23`：断言 `http://token:8080` 里的端口
  保留 **且** `://access_token=SECRET` 仍被遮），加一个可编译变异（把分段改回三次 `ReplaceAll`
  → 该用例必红）。
- **R2｜值类量词与交替改变既有匹配语义。** RE2 无回溯、线性时间，**不构成 ReDoS**；但交替的
  **优先级**（左分支优先）与引号组的贪婪消费会移动一些既有输入的期望值 —— rev1 已经发生过一次
  （L11 期望值写错）。**缓解**：§1.4 的**8 组边界锁定**全部先固化再改（M28/F 的交接前置）；
  引号组必须**捕获回写**；改动后逐条比对 §3.1 的「回归」行。
- **R3｜F2 的「吞到空白」在 query 串上过度脱敏，且**是本次第一种行为突变**。**
  `?a=1&password=&b=2` → `?a=1&password=***`（`b=2` 被吃掉）。**缓解**：`CHANGELOG.md`
  行为突变告知写清「值首字符是 `&`/`,`/`;` 时遮盖范围到下一个空白」；`08-部署运维.md` §8.4.3
  补一句；明确这是**刻意的**价值排序（宁可丢信息，不回显原串）。
- **R4｜分段把「现状遮住的」变成明文（覆盖面收窄且方向相反）。** 触发条件是 **host 段本身形如
  `key=value`**：`http://access_token=SECRET` 现状 `http://access_token=***`，无 F3b 的分段
  是 `http://access_token=SECRET`。rev1 把它错描为「将来有人加保留 path 前缀才出现」——**实测
  证明绝对 scheme 今天即可触发**，且该文本能进 `err.Error()`（`*url.Error` 会带完整 URL），
  经 `integration_handler.go:137/190/229` 的 `apierr.BadRequest` **进 400 响应体**。
  **缓解**：F3b（host 含 `=` → `<invalid-url>`，与既有 `host:` 判据同构）；守门用例
  `TestText_URL段不参与规则23` 增一条断言「`http://access_token=SECRET` 不含 `SECRET`」；
  `Text`/`URL` 注释写明「分段依赖 URL() 丢弃 path/query/userinfo **且不保留凭据形状的 host**」；
  `TRAPS.md` 记 T-56。
- **R5｜F4 的中文过度脱敏（第二种行为突变）。** 中文无词间空格 → `重置 password：请联系管理员`
  → `重置 password：***`，整句丢信息。**缓解**：实测代价写入 `CHANGELOG.md` 行为突变告知；
  该改动是**可单点回退**的（删 1 个字符 + 回滚 L12 的用例与台账行），F1–F3 不受影响；
  §7 已把它标为审查新增项，便于用户复核。

## 5. 验证清单

### 5.1 门槛与用例

- **第一步（先做）**：把 §1.4 的 **8 组** + §1.1 的 **L1–L12** 固化成表驱动用例，**先跑一遍确认
  「漏」的用例当前是红的** —— 这是「用例真的钉住了缺陷」的证明，而不是事后补一条恒绿的断言。
  - **断言用凭据字面量，不是统一的 `SECRET`**：L10 的输入是 `password=""abc`，凭据是 `abc`，
    写成 `assert.NotContains(out, "SECRET")` 会**恒绿**（正确性审查 #2 实测）。
  - **L13 是登记项**：用例断言**当前输出**（`{"password":***"SECRET"]}`），钉住不回归，
    并在注释里指向 TODO G-46 —— 它**不会**在修前变红，这是刻意的。
- 每个修复点至少 1 条走真实分支的用例：F1（引号 + 多空白 + Tab + 未闭合）、F2（引号 + 分隔符 +
  空值 + `&next=SECRET` 不产生新明文 + 引号形态回写）、F3+F3b（端口保留 + `://access_token=SECRET`
  仍被遮 + `http://access_token=SECRET` 不含凭据）、F4（全角冒号 + 中文过度脱敏的**既定**输出）。
- **变异反证**：逐条删掉/改回 F1、F2、F3、F3b、F4 的**关键 token**（保证可编译，T-31），
  对应用例必须**红在断言上**。编号沿用 M29 风格：**`M30-M1`…**
- 全量：`go test ./... -count=1`（27 包）—— `redact` 的调用面广，任何既有断言被这次改动移动
  都要在这一步暴露，**不许**为了让门禁变绿去改无关断言（那说明改动跑出了 §0 的范围）。

### 5.2 守门用例（防回归，独立于修复项）

| 用例 | 断言 |
|---|---|
| `TestText_URL段不参与规则23` | `http://token:8080/x` → 端口保留；`://access_token=SECRET` → 仍被遮；**`http://access_token=SECRET` → 不含 `SECRET`**；`http://user:pass@host/x` → `http://host` |
| `TestText_引号凭据全形态被遮` | §1.1 L1–L6 全部 `NotContains` 各自凭据字面量 |
| `TestText_值首字符为分隔符` | L7–L9 被遮；`password=&next=SECRET` **不**含 `=SECRET`；`?a=1&password=&b=2` 的期望值**显式写成** `?a=1&password=***`（把行为突变钉在测试里，而不是留给下一个人猜） |
| `TestText_引号形态回写` | §1.4 的三条引号锁定（`token="***`、`password="***"`、`{"password":"***"}`）—— 引号组写成非捕获会立刻红 |

### 5.3 台账

- `TODO.md`：G-34 / G-35 结案（文件:行 + 变异编号 + 行为突变）；新增 **G-46**（L13 结构化值）。
- `TRAPS.md`：新增 **T-56**（已核对当前最大编号为 T-55）—— 「**同一个函数的输出又是另一个规则的
  输入**时，两条规则的边界会互相咬合；修一条必须同时证明另一条没被移动」，与 T-52（两侧共享语义）、
  T-55（顺序也是语义）互引。
- `CHANGELOG.md`：M30 条目 + **行为突变告知**（① 值首字符是 `&`/`,`/`;` 时遮盖范围到下一个空白；
  ② 全角冒号也成为键值分隔符）。
- **仓库根** `08-部署运维.md` §8.4.3：补一句「URL 塌缩后不再被键值规则二次处理（端口保留），
  且 host 含 `=` 的形态塌缩为 `<invalid-url>`」。
- 本文档 §7（审查记录）、§8（实现记录）。

## 6. 不做的事（明确排除）+ 已实测登记的残余

**不做**：

1. **不换状态机**（方案 C）：收益是「根治」，代价是重写 G-28 刚收敛的三条规则 + 全部调用面
   重新验证；本轮的缺陷用「值类边界 + 分段」已能闭合，属过度设计。
2. **不修 `//token:8080/x`**（scheme-relative 且 host 不带点）：规则 1 的 scheme-relative 分支
   **刻意**只认带点域名/localhost/IPv6（避免把 `//var/log` 当 URL），放宽它会换来一批误伤。
3. **不修 `Authorization: Bearer ""`**：空凭据无内容可漏；为它加分支会让值类允许空匹配，
   徒增零长匹配的复杂度。
4. **不动键词表**：`token`/`pwd`/`sign` 这类短词在 host 名里会误伤，但把词表收窄会直接削弱
   脱敏覆盖面 —— 误伤由 F3 的分段消解（host 段不再走规则 3），不需要动词表。
5. **不引入新依赖**（保持 `regexp` + 标准库）。
6. **不改调用点**：`Text` 的签名与语义契约不变（除 §3.1 标出的行为突变）。

**已实测登记（本轮不修，全部有实测输出，不是推演）**：

| 残余 | 实测 | 为什么不修 |
|---|---|---|
| 结构化值（JSON 数组/对象）内元素明文：`{"password":["SECRET"]}`、`{"password":{"a":"SECRET"}}` | `{"password":***"SECRET"]}` | 需嵌套匹配 → 属第 1 条；**新增 TODO G-46** |
| 值**中间**的引号截断：`password=SEC"RET` | `password=***"RET` | 收窄引号只在值首起作用，会与 §1.4 的「引号形态回写」冲突 |
| 引号内含空格：`Authorization: Bearer "SECRET VALUE"` | 现状**整条明文** → 候选 `Bearer *** VALUE"`（首段已遮，尾部残留） | 真实 Bearer token 不含空格；候选相对现状是**净改进**，尾段登记 |
| 键名不落在 `-`/`_` 分段边界：`clientsecret=`、`_password=`、`1password=` | 三条均原样 | 键词表的既有取舍（代码注释已写明 `design=`/`monkey=` 同理），收窄会削弱覆盖面 |

## 7. 审查记录（三路对抗审查 → 逐条复现 → 处置）

2026-09-12 一轮三视角对抗审查；**每条发现都由我独立复现**（探针 `/tmp/m30probe`，`Text()` 与候选
实现对照），未复现的不采纳。

| # | 来源 | 发现 | 我的复现 | 处置 |
|---|---|---|---|---|
| 1 | 安全 | F3 分段把 `http://access_token=SECRET` 从「已遮」变**明文** | ✅ `noF3b` 实测 `http://access_token=SECRET` | **采纳（阻断）** → F3b；`http://access_token=SECRET:8080/x`、`redis://access_token=SECRET/0`、`http://token=x` 同族 |
| 2 | 安全 | §1.1 未穷尽：JSON 数组元素明文 | ✅ `{"password":***"SECRET"]}` | **采纳** → L13，登记 G-46（修它需嵌套匹配） |
| 3 | 安全 | 值中间引号截断 `password=SEC"RET` | ✅ `password=***"RET` | **采纳** → §6 残余表 |
| 4 | 安全 | 全角冒号 `password：SECRET` 明文 | ✅ 原样 | **采纳** → F4（附代价 R5） |
| 5 | 安全 | 引号内含空格只遮首段 | ✅ 现状整条明文 / 候选 `Bearer *** VALUE"` | **部分采纳** → §6 残余表（候选是净改进） |
| 6 | 安全 | `["']{0,2}` 上界任意，3 引号仍漏 | ✅ `password='''SECRET` | **采纳** → 改用 `["']*`（F1/F2 对称） |
| 7 | 安全 | `clientsecret=` 键名不命中 | ✅ 原样 | **采纳（登记）** → §6 残余表，非本轮范围 |
| 8 | 正确性 | §3.1 的 L11 期望值写错（`'***'` vs `'"***'`） | ✅ `password= '"***'` | **采纳** → 已改 §3.1 |
| 9 | 正确性 | §5.1 「L1–L11 统一 `NotContains SECRET`」对 L10 恒绿 | ✅ 凭据字面量是 `abc` | **采纳** → §5.1 改为按凭据字面量断言 |
| 10 | 正确性 | F2 候选实现（捕获回写）确实达到期望；`password=&` 无零长匹配 | ✅ 全部对上（除 #8） | **确认**，实现注意已写入 §2.2 |
| 11 | 正确性 | §1.2 末行「或串结束」措辞不准 | ✅ `http://secret` 原样、`http://secret:8080` 命中 | **采纳** → §1.2 已改（必须是 `:`/`=`） |
| 12 | 一致性 | §5.1「§1.4 的 5 组」与实际 8 行不符（数字来自 M28/F 的另一张表） | ✅ §1.4 实为 8 组 | **采纳** → 已改 8 组 |
| 13 | 一致性 | 缺 Where（变更清单） | ✅ 本文件确无 | **采纳** → 新增 §2.5 |
| 14 | 一致性 | 缺 §0 流程缩放决策、覆盖率门槛、状态行 | ✅ 均缺 | **采纳** → §0 / 抬头已补 |
| 15 | 一致性 | 新 trap 编号未钉死 | ✅ 最大为 T-55 | **采纳** → 写死 **T-56** |
| 16 | 一致性 | 变异编号未约定、`08-部署运维.md` 未标明在仓库根 | ✅ | **采纳** → §5.3 写明 `M30-Mn` 与「仓库根」 |
| 17 | 一致性 | 调用点、规则语义、对 M28/F 与 TODO 的引用**逐条核实为真** | ✅ | 确认，无需改 |

**我自己复现时额外发现（审查未提）**：F2 的引号组若写成**非捕获**，§1.4 的三条锁定会全破
（实测 `password="SECRET"` → `password=***"`、`token="S` → `token=***`、`{"password":"ab}c"}` →
`{"password":***"}`）→ 已写入 §2.2 实现注意 + §5.2 专门用例。

**未采纳的**：无（17 条全部复现成立；#5 降级为登记）。

## 8. 实现记录

**提交链**：`f62b8fd`（本文档 rev2）→ `b05df00`（先固化 §1.4 边界锁定 + 回归面 + 已知残余钉子，
全绿）→ `8b03232`（F1 / F2 / F3+F3b / F4）→ `7253331`（审计迭代：两族净回归 + F4b + F3b 同集）。
`b05df00` 单独成一个提交，是为了同时满足「门禁全绿才 push」与「先固化用例再改实现」
（固化用例本身不改生产代码，可独立验证）。

### 8.1 逐项 before/after（全部实测输出）

`before` = `git show b05df00:backend/internal/redact/redact.go` 的真实输出。

| 项 | 输入 | before | after |
|---|---|---|---|
| F1 | `Authorization: Bearer "SECRET"` | 原样（**明文**） | `Authorization: Bearer ***"` |
| F1 | `Authorization: Bearer "SECRET` | 原样（**明文**） | `Authorization: Bearer ***` |
| F1 | `Authorization: Bearer ""SECRET` | 原样（**明文**） | `Authorization: Bearer ***` |
| F2 | `password=&SECRET` | 原样（**明文**） | `password=***` |
| F2 | `password=;SECRET` / `,SECRET` | 原样（**明文**） | `password=***` |
| F2 | `password=""abc` | 原样（**明文**） | `password=""***` |
| F2 | `password= '"SECRET'` | 原样（**明文**） | `password= '"***'` |
| F2（锁定） | `token="S` | `token="***` | `token="***`（不变） |
| F2（锁定） | `password="SECRET"` | `password="***"` | `password="***"`（不变） |
| F3 | `http://token:8080/x` | `http://token:***`（端口被规则 3 吃掉） | `http://token:8080` |
| F3 | `redis://pwd:6379/0` | `redis://pwd:***` | `redis://pwd:6379` |
| F3b | `http://access_token=SECRET` | `http://access_token=***`（**靠串联的偶然兜底**） | `<invalid-url>` |
| F4 | `password：SECRET` | 原样（**明文**） | `password：***` |
| F4b | `password=="SECRET"` | `password=***"SECRET"`（**明文尾部**） | `password=="***"` |
| 净回归① | `password=https://example.com` | `8b03232` 版：`password=https://example.com`（**明文**） | `password=***` |
| 净回归② | `password==http://SECRET/x` | `8b03232` 版：`password=***http://SECRET`（**明文**） | `password==***` |

F3b 的 before/after 都是「不泄漏」，差异在信息量：旧版是**偶然**遮住的（规则 3 把 `access_token=SECRET`
当键值抹成 `***`），一旦有人改动串联就会变明文 —— 这正是净回归①的成因。`<invalid-url>` 是把
「这是凭据形状、不是主机」写进**结构**，不再依赖另一条规则的行为。

### 8.2 变异反证（T-31：每条先 `go build`，编译失败判 INVALID 不算数）

12 条全部**红在断言上**（无存活、无编译失败），跑完还原逐字回到运行前（内容哈希比对）：
`/tmp/m30mutate.py`。

| 编号 | 变异 | 结果 |
|---|---|---|
| M30-M1 | F1 回退：规则 2 值类不吃起始引号 | 红，9 条用例 |
| M30-M2 | F2 引号组写成非捕获（不回写） | 红，15 条（含 §1.4 三组锁定） |
| M30-M3 | F2 去掉「起分隔符」分支 | 红，10 条 |
| M30-M3b | F2 起分隔符只吞一个 token | 红，5 条 |
| M30-M4 | F3 回退：不分段，先过规则 2/3 再塌缩 URL | 红，11 条 |
| M30-M5 | F3 写反：URL 段的输出又送回规则 2/3 | 红，11 条 |
| M30-M6 | F3b 去掉：host 含分隔符不再塌缩 | 红，8 条 |
| M30-M7 | F4b 回退：分隔符只收一个 | 红，7 条 |
| M30-M7b | F4 回退：分隔符不收全角 | 红，6 条 |
| M30-M8 | 净回归修复回退：段尾会被规则吃满时照切 | 红，13 条 |
| M30-M9 | 哨兵换成值类排除字符（`"`） | 红，12 条 |
| M30-M10 | F3b 判据收窄回半角 `=` | 红，3 条 |

### 8.3 差分反证（本轮的关键证据 —— 只跑新增用例**证明不了**没有净回归）

判据：同一张用例表喂两版实现，**旧遮住而新明文 = 净回归**。旧实现逐字取自
`git show b05df00:backend/internal/redact/redact.go`，表为 12 前缀 × 8 分隔符 × 20 值 × 6 后缀
= **11520 组**（`/tmp/m30diff`，可重跑）。

| 版本 | 净回归 | 真泄漏 | 说明 |
|---|---|---|---|
| `8b03232`（F1–F4） | **有** —— 审计两路独立报出，差分扫出 162 条 | — | 全在同一族：`<敏感键>=<URL>`（值本身是 URL） |
| 第一版修法（`danglingCredRe` 前缀模式） | **162 → 第二族现形**（`password==http://SECRET/x`） | — | 前缀模式与规则 2/3 不同集（漏引号形态、漏重复分隔符） |
| **最终版**（`eatsFollowing` 问规则本身 + F4b + F3b 同集） | **0** | **0** | 阈值 0 是通过条件，不是「比之前少」 |

差分同时暴露一族**既有**泄漏（旧版同样漏，非本轮引入）：`password=="SECRET"` → `password=***"SECRET"`，
与 F2 要修的 `password=""abc` 同族 → 已用 F4b（分隔符量词 `+`）一并修掉，并写进行为突变告知。

### 8.4 真 PG / 迁移

**本轮无 schema 变更、无迁移**（纯函数包 + 测试）。`./scripts/db_smoke.sh` 照跑：
全新迁移 + 升级路径 + 回滚 + 冒烟断言全部通过 —— 用来证明「没有迁移」这件事本身没被破坏。

### 8.5 门禁

`gofmt -l ./internal ./cmd ./tests` 空 / `go vet ./...` 干净 / `go build ./...` ok /
`go test ./... -count=1` **27 包全绿**（`redact` 调用面广，无外部断言被移动）/
`./scripts/db_smoke.sh` ✅ / `npx tsc --noEmit` 0（前端未改，按门禁照跑）。

### 8.6 实现后审计（两路：正确性 / 安全）与处置

| # | 来源 | 发现 | 我的处置 |
|---|---|---|---|
| 1 | 正确性 + 安全（**独立报出同一条**） | F3 分段把 `<敏感键>=<URL>` 的值从「已遮」变**明文**（`password=https://example.com`） | **采纳（阻断）** → 复现确认 → 修（`eatsFollowing`）→ 补用例 → 变异 M30-M8/M9 |
| 2 | 差分（我自查，非审计提） | 同族第二形态：重复分隔符 `password==http://SECRET/x` 仍明文 | **采纳** → 同上修法覆盖（前缀模式换成问规则本身） |
| 3 | 差分（我自查） | 既有泄漏：`password=="SECRET"` → `password=***"SECRET"` | **采纳** → F4b（分隔符量词 `+`）+ 行为突变③ |
| 4 | 差分（我自查） | F3b 判据与规则 3 分隔符类不同集：host 含全角 `：`/`＝` 时原样回显 | **采纳** → 拒绝集改 `ContainsAny(u.Host, "=＝：")` + M30-M10 |
| 5 | 安全 | 全角等号 `＝` 未收（`password＝SECRET` 明文） | **采纳** → 并入 F4（与全角冒号同源）+ M30-M7b |

**教训（已写入 `TRAPS.md` T-56）**：一条规则的输出是另一条规则的输入时，两条边界互相咬合；
拆开串联必须**用差分证明**另一条没被移动。「另写一份必须与既有规则逐字同集的模式」是错的解法 ——
M30 试过两次（前缀模式漏了两族），最终改为**问规则本身**（哨兵试探）。

### 8.7 残余（本轮不修，全部实测）

见 §6 表；新增 **TODO G-46**（结构化值内元素明文，`{"password":["SECRET"]}` →
`{"password":***"SECRET"]}`，需嵌套匹配）。`TestText_已知残余_钉住` 三条断言钉住当前输出：
修它们时测试会红，从而必须同步 TODO 与本文件。
