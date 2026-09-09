# 修复方案：让 `docker compose up` 真能跑通（TODO G-9 + G-10 + G-13）

> 状态 **rev2（审查后修订）** · 日期 2026-09-09 · 来源 `TODO.md` G-9 / G-10，及实测新发现的 G-13
> 分支 `main`（用户已授权直推主干）
> 前序：`docs/FIX-PLAN-TRUSTED-PROXY.md`（G-7）——本方案的端到端验证正是 G-7 的真实拓扑验证
> 审查：**安全/运维视角**（B-1..B-6 / S-1..S-7 / I-1..I-4）+ **正确性视角**（M-1..M-4 / S-1..S-2 / I-1..I-10），
> 两轮均为只读实证（真实容器/网络/postgres/viper 探针），仓库零改动。逐条处置见 **§7**。

## 1. 问题（What / Why）

文档（README badge、`08-部署运维.md`）把 `docker compose up -d` 当作开箱部署路径，
但**这条路径当前必然失败**，且「用环境变量注入 secret」这条路本身是断的。四组事实：

### 1.1 G-9：仓库里没有任何 Dockerfile

`docker-compose.yml:39-41`、`:63-65` 声明 `build: ./backend` / `./frontend` + `dockerfile: Dockerfile`，
而 `git ls-files | grep -i dockerfile` 与全盘 `find` 均为空 → `docker compose build` 直接失败。

> 根因之一：`.gitignore:48-49` 的裸 `Dockerfile` / `.dockerignore` 规则匹配任意深度，
> 当年就算写了也进不了仓库（M-1/B-1）。本轮必须同时改 `.gitignore`，否则修完仍不生效。

### 1.2 G-10：compose 传给 api 的环境变量一个都不生效

```yaml
# docker-compose.yml:46-50（现状）
- DATABASE_URL=postgres://nmp:nmp123@postgres:5432/network_monitor
- REDIS_URL=redis://redis:6379/0
- NETBOX_URL=http://netbox:8000
```

`config.Load()` 只认 `NMP_` 前缀 + mapstructure 路径（`NMP_DATABASE_HOST`、
`NMP_INTEGRATIONS_NETBOX_URL`…）。**这五个变量一个都不会被读到**，api 会退回
`config.yaml` 里的 `localhost` 并连不上库。同时缺 `NMP_AUTH_JWT_SECRET` /
`NMP_DATABASE_PASSWORD` / `NMP_AUTH_API_KEY_PEPPER`，`Validate()` 必拒启。

另：`frontend/nginx.conf:14` 的 `proxy_pass http://backend:8080/api/` 指向主机名 `backend`，
compose 里该服务叫 `api`（`container_name: nmp-api`）→ 容器内 DNS 解析不到
（已实证：compose 网络里 `container_name` **不注册 DNS**，必须靠服务名或网络别名）。

### 1.3 G-13（实测新发现，**比 G-10 更硬**）：`auth.api_key_pepper` 键缺失 → env 被静默忽略

viper 的 `Unmarshal` 只遍历 `AllKeys`（yaml 键 + `SetDefault` 键），**纯环境变量键不进 AllKeys**。
`backend/config.yaml` 有 `auth.jwt.secret`、`database.password` 的占位，但**没有 `auth.api_key_pepper`**，
于是即使运维按文档注入 `NMP_AUTH_API_KEY_PEPPER`，它也读不进来。实测（仓库自带 config.yaml）：

```console
$ NMP_AUTH_JWT_SECRET=<40字符> NMP_AUTH_API_KEY_PEPPER=<40字符> NMP_DATABASE_PASSWORD=probe-pw \
    go run ./cmd/migrate status
2026/09/09 09:59:06 配置加载失败: 配置校验失败: auth.api_key_pepper 不能为空（通过 NMP_API_KEY_PEPPER 注入）
exit status 1
```

两个独立缺陷叠在一起：

| # | 缺陷 | 后果 |
| --- | --- | --- |
| G-13a | yaml 缺 `auth.api_key_pepper` 键 | 该 secret **无法**通过 env 注入 → 服务永远起不来 |
| G-13b | 报错信息写 `NMP_API_KEY_PEPPER`（正确是 `NMP_AUTH_API_KEY_PEPPER`） | 运维照提示修，仍然起不来 |

> G-7 修 `trusted_proxies` 时踩过同一个坑（yaml 必须有 `trusted_proxies: []` 占位，V-6 单测钉死）。
> 这次把「每个必须靠 env 注入的 secret，yaml 里都要有占位」上升为一条可测试的约束（D-A）。
> 已逐字段核对 `Config` 各 struct 的 mapstructure 键 vs shipped `config.yaml` vs `Validate()`：
> **唯一缺口就是 `api_key_pepper`**（I-3），其余必填项都有占位键。

### 1.4 默认 `up` 会把带占位凭据的辅助服务一并拉起到宿主（B-5/B-6）

`netbox`（`SECRET_KEY=your-secret-key-here-…`）、`graylog`（占位 secret + 非法 SHA2，
起不来）、`elasticsearch`（`xpack.security.enabled=false`）、`redis`（无 `requirepass`）
全部无 profile 门控、端口绑 `0.0.0.0`。`docker compose up -d` 会把这些一起带上，
且 `postgres` 只有 `nmp` 一个角色，主链密码一改强，aux 仍用硬编码 `nmp123` → 连不上。

### 1.5 附带发现（smoke 实测）：三个 CLI 在真实 postgres 上必失败

`scripts/smoke-compose.sh` 第一次跑，主链 4 步全绿（up / `/readyz` / `schema_migrations=13`），
第 5 步 `admin-bootstrap` 失败。根因**与 compose 无关，是产品缺陷**：

`cmd/server` 与 `cmd/migrate` 会 `database.SetMigrationsFS(...)`，而 `cmd/admin-bootstrap`、
`cmd/seed`、`cmd/set-role` **没有注入** → `database.Init` 走 `else` 分支的 gorm `AutoMigrate`
兜底（`internal/database/database.go:71-81`），在真实 postgres 上：

| 场景 | 现象（实测） |
| --- | --- |
| 空库 | AutoMigrate 建表但**不建种子角色** → `未找到 admin 角色，请先运行 migrate up` |
| 已迁移的库 | AutoMigrate 与迁移 DDL 漂移 → `insufficient arguments`（`database.go:106`） |

即 `admin-bootstrap` / `make db-seed` / `set-role` 在真实 postgres 上**从来没有成功过**——
因为在此之前仓库里没有 Dockerfile，这条路径从未被真正执行。

**修复（D-I）**：三个 CLI 各加一行 `database.SetMigrationsFS(network_monitor_platform.MigrationsFS)`，
让它们走生产同一条迁移路径（幂等 + advisory lock 保护）。实测：全新空库上
`admin-bootstrap` 应用 13 条迁移并建号成功。

> 兜底 `AutoMigrate` 本身在 postgres 上不可用（表结构漂移），登记 **G-18**：
> 要么删掉兜底改为显式报错，要么限定为 sqlite 测试专用。

## 2. 方案（How）

### D-A 修 G-13：yaml 占位 + `SetDefault` + 正确报错 + 单测钉住「shipped yaml + env 能启动」

1. `backend/config.yaml` 的 `auth:` 下补 `api_key_pepper: ""`（带注释说明必须 env 注入）。
2. `config.go` 报错文案改为 `NMP_AUTH_API_KEY_PEPPER`（与 viper 实际键名一致）。
3. `Load()` 里 `server.trusted_proxies` 旁补 `viper.SetDefault("auth.api_key_pepper", "")`
   （S-1）：**生产按文档挂载自定义/旧 config.yaml 时，env 才照样生效**——只加 shipped yaml
   占位只能救「用镜像内 config.yaml」的场景。
4. 新增单测：
   - `TestLoad_ShippedConfigYAML_必填Env齐备时成功`：直接读**仓库里的** `backend/config.yaml`，
     设三个必需 env，断言 `Load` 成功且值都从 env 生效。以后谁新增「必须 env 注入」的字段却忘加
     yaml 占位，这条会红。
   - 与 `config_test.go:499` 对称的「yaml 无 pepper 键 + env → 成功」用例（钉死 `SetDefault` 路径）。
   - 报错文案断言含 `NMP_AUTH_API_KEY_PEPPER`。

> **排期**：D-A 独立成**第一个提交**先落（S-2）。否则 D-E 教运维设的
> `NMP_AUTH_API_KEY_PEPPER` 仍是「设了也白设」——`${VAR:?}` 只是插值层，不保证 viper 读得进。

### D-B `backend/Dockerfile`（多阶段，非 root）

```dockerfile
FROM golang:1.25-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download                      # 依赖层独立缓存
COPY . .
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/server ./cmd/server \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/migrate ./cmd/migrate \
 && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/admin-bootstrap ./cmd/admin-bootstrap

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata && adduser -D -u 10001 nmp
WORKDIR /app
COPY --from=build /out/ /app/
COPY config.yaml ./
USER nmp
EXPOSE 8080
HEALTHCHECK --interval=15s --timeout=3s --start-period=10s --retries=5 \
  CMD wget -qO- http://127.0.0.1:8080/healthz || exit 1
CMD ["./server"]
```

- **`CMD` 而非 `ENTRYPOINT`**（M-2）：rev1 用 `ENTRYPOINT ["./server"]` + 服务侧
  `command: ["./migrate","up"]` 会拼成 `./server ./migrate up`（Docker 语义：`command` 设的是 CMD，
  与 exec 数组 ENTRYPOINT 拼接），migrate 起成 server、`up` 死锁。本轮已砍掉 migrate 服务（D-C），
  但 `CMD` 仍是对的形式：镜像可被 `docker run <img> ./migrate up` 正确覆盖。
- `CGO_ENABLED=0`：生产路径**零 `import "C"`**（实测 `CGO_ENABLED=0 go build` 成功、静态链接）。
  注：`mattn/go-sqlite3` 只被 `*_test.go` 引用，不进二进制——rev1 说的「sqlite 驱动是 pure-Go」
  表述不实，已改（I-4）。
- 基础镜像钉 `major.minor`；摘要（digest）钉死列为「可选加固」，不默认做（每次升级都要改，
  维护成本 > 收益，且本仓库 CI 不 build 镜像）。再评估周期写进 TODO。
- 镜像内 `COPY config.yaml` 会把占位值带进镜像层（低敏），文件内注释已标「必须 env 覆盖」。
- 顺带 build `admin-bootstrap`（S-3）：smoke 脚本要在全新空库上建号才能验证**认证成功**的完整链路。

### D-C 迁移执行：**砍掉独立 migrate 服务**，沿用 api 启动自迁移（rev1 方案 B 作废）

rev1 选了「独立 `migrate` 服务 + `service_completed_successfully`」。两轮审查各自实证后，
该方案的论证不成立，**撤销**：

| 事实 | 证据 | 对 rev1 的影响 |
| --- | --- | --- |
| `database.Init` **无条件** `migrate.Up` | `database/database.go:71-75` + `cmd/server/main.go:34` 注入 `MigrationsFS` | 「不 entrypoint 跑迁移」做不到；单镜像 `docker run` **本来就会**迁移，rev1 表 B 的劣势项是错的（M-3） |
| 迁移锁是**非阻塞** `pg_try_advisory_lock` | `migrate/migrate.go:70-78` | 多副本并发**没有**被独立服务解决；抢不到锁的副本照样 `Fatal` 重启（B-2） |
| `cmd/migrate` 走全量 `config.Load`→`Validate` | `cmd/migrate/main.go:30-33` | migrate 容器必须注入 jwt/pepper 全部 secret，否则 gate 永不满足、api 永不启动（B-3） |

要让迁移真正可分离，得新增 `database.automigrate` 开关（新配置键 + `database.Init` 代码改动 +
单测）——**超出本轮**「让 compose 跑通」的范围，且新键又要防 G-13 的 viper 坑。
两轮审查都给出了同一个退路：**退回「server 自动迁移 + 文档」**。

- api 继续在启动时迁移（既有行为，零改动），`depends_on: postgres healthy` 保证库已就绪。
- 文档写明：**api 单副本约束**（多副本冷启动时抢不到 advisory lock 的副本会启动失败），
  多副本/迁移解耦方案登记 **G-14**。
- 因此 V-9 的「migrate 服务失败则 api 不启动」不再适用，改为断言「api 起来后
  `schema_migrations` 有 13 条」（V-9'）。

### D-D `frontend/Dockerfile`（node 构建 → nginx 托管）

```dockerfile
FROM node:22-alpine AS build
WORKDIR /app
COPY package.json package-lock.json ./
RUN npm ci
COPY . .
RUN npm run build

FROM nginx:1.27-alpine
COPY --from=build /app/dist /usr/share/nginx/html
COPY nginx.conf /etc/nginx/conf.d/default.conf
```

- `npm ci`（锁文件精确安装，`integrity` sha512）+ 两个目录各加 `.dockerignore`
  （`node_modules`、`dist`、`bin`），否则宿主机 `node_modules` 会覆盖镜像内的，且构建上下文巨大。
- `nginx.conf` 里 `backend` 与 compose 服务名不一致的问题在 D-E 解决，**不改 `nginx.conf`**
  （它同时是「非 compose 部署」的模板，改名会让外部部署失效）。

### D-E `docker-compose.yml`：env 改 `NMP_*`、secret 必填、upstream 对齐、端口收敛、aux 隔离

```yaml
  api:
    environment:
      - NMP_SERVER_HOST=0.0.0.0
      - NMP_SERVER_MODE=${NMP_SERVER_MODE:-debug}     # 生产必须显式设 release（见 R-5）
      - NMP_DATABASE_HOST=postgres
      - NMP_DATABASE_PORT=5432
      - NMP_DATABASE_USER=nmp
      - NMP_DATABASE_PASSWORD=${NMP_DATABASE_PASSWORD:?必须在 .env 里设置}
      - NMP_DATABASE_NAME=network_monitor
      - NMP_DATABASE_SSLMODE=disable
      - NMP_REDIS_HOST=redis
      - NMP_INTEGRATIONS_NETBOX_URL=http://netbox:8000
      - NMP_INTEGRATIONS_ZABBIX_URL=http://zabbix:8080
      - NMP_INTEGRATIONS_GLPI_URL=http://glpi:80
      - NMP_AUTH_JWT_SECRET=${NMP_AUTH_JWT_SECRET:?必须在 .env 里设置}
      - NMP_AUTH_API_KEY_PEPPER=${NMP_AUTH_API_KEY_PEPPER:?必须在 .env 里设置}
      - NMP_SERVER_TRUSTED_PROXIES=172.28.0.10
    ports:
      - "127.0.0.1:8080:8080"     # 仅环回：给本机调试/smoke 用，不暴露到局域网
    networks:
      default:
        aliases: [backend]        # 让 frontend/nginx.conf 的 http://backend:8080/api/ 解析到本服务
```

- `postgres` 的 `POSTGRES_PASSWORD` 与 `NMP_DATABASE_PASSWORD` **同源同变量**，
  避免「库密码改了、api 没改」。
- `${VAR:?msg}`：缺值直接报错退出，**不再有 `nmp123` 硬编码凭据进仓库**。
- **端口收敛（B-6）**：`redis` **不发布**（宿主机已有 redis 占用 6379，发布必然
  `port is already allocated`；且无密码）；`postgres` 绑 `127.0.0.1:5432`（**必须**发布到环回——
  `make deploy` 的 `db-migrate`/`db-seed` 在宿主机上跑、要连 localhost:5432）；`api` 绑 `127.0.0.1:8080`；
  `web` 绑 `127.0.0.1:3000`（**安全审计 P1 修订**：web 提供的是明文 HTTP，绑 `0.0.0.0` 等于把
  未加密登录入口暴露到局域网，与 PCI DSS TLS 1.2+ 要求冲突；对外必须前置 TLS 终结）。
  调试走 `docker compose exec`。
- **aux 隔离（B-5）**：`netbox`/`zabbix`/`glpi`/`graylog`/`elasticsearch`/`mongoDB` 全部加
  `profiles: ["aux"]` → 默认 `up -d` 只起主链（postgres/redis/api/web），aux 要
  `docker compose --profile aux up -d` 显式拉起。占位凭据因此**不会随默认命令上线**。
  netbox/zabbix 的 DB 密码同步改为 `${NMP_DATABASE_PASSWORD:?}`，与主链保持同源。
- 删掉 compose 顶部的 `version: '3.8'`（compose v2 已废弃该键并打 warning，I-6）。
- 不改 `container_name`（`nmp-*` 是既有约定，smoke 脚本要按前缀检测）。

### D-F CI 守卫：`compose-config` job（作用范围按实证收窄）

```yaml
permissions:
  contents: read          # 三个 job 都只需读；收紧默认 token 权限（S-5）

  compose-config:
    name: Compose config
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Dockerfile 存在性
        run: |
          test -f backend/Dockerfile && test -f frontend/Dockerfile
          test -f backend/.dockerignore && test -f frontend/.dockerignore
      - name: Render compose file
        env:
          NMP_DATABASE_PASSWORD: ci-dummy-password
          NMP_AUTH_JWT_SECRET: ci-dummy-jwt-secret-at-least-32-bytes!!
          NMP_AUTH_API_KEY_PEPPER: ci-dummy-pepper-at-least-32-bytes!!
        run: docker compose config -q
```

**作用范围（诚实版，B-4/M-4）**：`docker compose config -q` 实测只能抓
**YAML 语法、`${VAR:?}` 缺值、compose schema**——它**不 stat** build 上下文/Dockerfile
（实测指向不存在的 Dockerfile 仍 exit 0），也**不懂** `NMP_` 语义（变量名拼错无感）。
所以：
- 「Dockerfile 漂移」由 `test -f` 那一行兜；
- 「env 名与 viper 键对齐」由 **D-A 的 backend 单测**兜（随 backend job 进 CI），
  不指望 compose job；
- `-q` 必须保留：不带 `-q` 的 `docker compose config` 会把解析后的 secret 明文打到 stdout（S-5）。

**不在 CI 里 `docker compose build/up`**：Go 与 Node 全量构建会给每次 push 加 3–5 分钟，
而 `dbsmoke` 已覆盖迁移、`backend` job 已覆盖编译。完整栈的真跑通放在本地脚本（D-H）。

### D-G 文档 + `.env.example`

- `08-部署运维.md` 新增「compose 真跑通」小节：`.env` 必填项、启动命令（**明确列出主链服务**，
  不是裸 `up -d`）、迁移随 api 启动的语义与单副本约束、aux profile 用法、
  端口暴露现状、生产注意事项（改 release、集成 token、`down -v` 重建库）。
- **同步 §8.3 模板与仓库 compose 的漂移（I-1）**：模板是「image 拉取 + profiles + 不发布端口」，
  仓库是「build + 全量发布 + 占位」。以仓库为准改文档。
- README badge / 快速开始里的 `docker compose up -d` 措辞同步为显式服务清单（I-9）。
- `backend/.env.example` 补 `NMP_AUTH_API_KEY_PEPPER`（G-10 遗留项）。
- 根目录新增 `.env.example`（compose 默认读根 `.env`），只放占位与生成命令，不提交真实值。

### D-H 端到端验证脚本 `scripts/smoke-compose.sh`（按 S-2/S-3/S-4 修订）

```text
0. COMPOSE_PROJECT_NAME=itmanager-smoke-<随机>，先检查 nmp- 容器/项目名冲突
1. 生成临时 .env（openssl rand 随机 secret）→ docker compose up -d --build postgres redis api web
2. 轮询 http://127.0.0.1:8080/readyz（真 ping DB）直到 200，超时 180s 判红
3. 断言迁移真的跑了：exec postgres psql -c 'select count(*) from schema_migrations' == 13
4. 建号：exec api ./admin-bootstrap（随机强密码）
5. 经 nginx 登录：curl -H 'X-Forwarded-For: 1.2.3.4' http://localhost:3000/api/auth/login
   → 断言 200（不是 401；401 说明建号/链路有问题，必须判红）
6. 查库断言 audit_logs.ip：
   硬不变量 = 既 ≠ 1.2.3.4（伪造值未被采信）也 ≠ 172.28.0.10（不是代理地址）；
   并打印实际值。在「默认 docker bridge NAT + 经 127.0.0.1 连入」时该值 == 网关 172.28.0.1，
   但 rootless / 关 userland-proxy / 非环回地址连入时实际值会变 → 不断言等于网关（S-2）。
7. 负例：NMP_SERVER_TRUSTED_PROXIES=1.2.3.4 重启 api → 审计 IP 退化为 172.28.0.10（代理），
   且日志出现一次「未受信来源的 XFF」WARN
8. 清理：默认 down -v --rmi local（脚本用随机项目名 + 拒绝与既有 nmp- 容器共存 +
   --project-name 限定，把误删风险收窄到「本脚本自己的项目」）；`--keep` 才保留容器/卷。
   若检测到非本脚本项目的 nmp- 容器在跑，拒绝执行 down -v
```

这条脚本同时是 **G-7 的端到端证明**：G-7 的单测用的是 `httptest` 伪造的 RemoteAddr，
而这里跑的是真实 nginx → 真实容器网络 → 真实 `ClientIP()` → 真实审计落库。

### D-I 修复三个 CLI 的迁移路径（§1.5）

`cmd/admin-bootstrap/main.go`、`cmd/seed/main.go`、`cmd/set-role/main.go`：
`config.Load` 之后、`database.Init` 之前加
`database.SetMigrationsFS(network_monitor_platform.MigrationsFS)`。

## 3. Where

| 文件 | 动作 |
| --- | --- |
| `.gitignore` | **删掉第 48-49 行裸 `Dockerfile`/`.dockerignore`**（M-1/B-1，先决条件） |
| `backend/cmd/{admin-bootstrap,seed,set-role}/main.go` | 注入 `MigrationsFS`（D-I，§1.5） |
| `backend/config.yaml` | 补 `api_key_pepper: ""`（D-A） |
| `backend/internal/config/config.go` | 报错文案改 `NMP_AUTH_API_KEY_PEPPER`；补 `SetDefault`（D-A） |
| `backend/internal/config/config_test.go` | 新增 shipped-yaml + SetDefault 用例（D-A） |
| `backend/Dockerfile`、`frontend/Dockerfile` | 新增（D-B/D-D） |
| `backend/.dockerignore`、`frontend/.dockerignore` | 新增 |
| `docker-compose.yml` | env 改 `NMP_*`、`${VAR:?}`、端口收敛、aux profiles、网络别名、删 `version:`（D-E） |
| `.github/workflows/ci.yml` | 新增 `compose-config` job + `permissions: contents: read`（D-F） |
| `backend/.env.example`、`.env.example` | 补/新增（D-G） |
| `scripts/smoke-compose.sh` | 新增（D-H） |
| `08-部署运维.md`、`README.md` | compose 真跑通小节 + §8.3 漂移对齐（D-G） |
| `TODO.md` | G-9/G-10/G-13 结案；新增 G-14..G-18（§6） |

## 4. 验证清单

| # | 验证 | 方式 |
| --- | --- | --- |
| V-1 | shipped `config.yaml` + 三个必需 env → `Load` 成功 | 新增单测（D-A） |
| V-2 | 缺 `NMP_AUTH_API_KEY_PEPPER` → 报错文案指向**正确**变量名 | 单测断言错误串 |
| V-3 | yaml 无 pepper 键 + env → 仍生效（`SetDefault` 路径） | 单测（S-1） |
| V-4 | `docker compose config -q` 用 dummy env 渲染成功 | 本地 + CI job |
| V-5 | 缺 `NMP_DATABASE_PASSWORD` → compose 直接报错（不是静默用空值） | 本地实测 |
| V-6 | `test -f` 四文件存在；`.gitignore` 不再吞 Dockerfile | CI job + `git check-ignore` |
| V-7 | `docker compose build` 两个镜像都成功 | 本地实测 |
| V-8 | 全栈起来后 `GET /readyz` == 200（真 ping DB） | D-H 脚本 |
| V-9' | api 起来后 `schema_migrations` == 13 条 | D-H 脚本 + psql 断言 |
| V-10 | **经 nginx 伪造 XFF → 审计 IP ≠ 伪造值、≠ 代理地址**（G-7 端到端） | D-H 脚本 + psql 断言 |
| V-11 | 故意把 `trusted_proxies` 配错 → 审计 IP 退化为代理地址 + 一次 WARN | D-H 脚本负例 |
| V-12 | 容器内进程非 root（`id` 不含 uid 0） | 本地实测 |
| V-13 | 默认 `up -d` 只起主链 4 个服务，aux 需 `--profile aux` | 本地实测 |
| V-14 | `docker compose down -v` 后磁盘回收，镜像清理 | 本地实测 + `docker system df` |
| V-15 | 三个 CLI 在真实 postgres 上可用（§1.5） | D-H 脚本步骤 5/5b（admin-bootstrap / set-role / seed 各建一次并断言库内结果）+ CI `dbsmoke` job 的 CLI 回归步（V-16） |
| V-16 | 去掉任一 CLI 的 `SetMigrationsFS` → 自动化变红（变异审计 E4 补的缺口） | CI `dbsmoke` job「CLI 迁移路径回归」步；本地用 `docker run postgres:18-alpine` 复跑同一组命令已验证 |

## 5. Risk（≥2 具体失败模式 + 缓解）

| # | 失败模式 | 缓解 |
| --- | --- | --- |
| R-1 | `${NMP_DATABASE_PASSWORD:?}` 是**破坏性**变更：老部署的 `.env` 若没有这个变量，`docker compose up` 从「能起」变成「直接报错」 | 文档给出迁移步骤（把原 `POSTGRES_PASSWORD` 的值改成 `NMP_DATABASE_PASSWORD`）；报错文案自带说明；CI 用 dummy 值保证渲染可测 |
| R-2 | 静态 IP + 固定子网与宿主机已有网段冲突 → `Pool overlaps` 起不来 | 已在 G-7 文档写明自检命令与换网段方法；D-H 脚本启动前先 `docker network inspect` 检查 |
| R-3 | 前端镜像构建依赖 npm registry；`npm ci` 在无网/镜像源变更时失败 | 构建层与依赖层分离（`package*.json` 先 COPY），失败信息明确；CI 不 build，避免主流程被外部网络拖红 |
| R-4 | **迁移随 api 启动**，多副本冷启动时抢不到 advisory lock 的副本启动失败（既有行为，本轮不改） | 文档写明单副本约束；G-14 登记解耦方案；单副本下 `depends_on: postgres healthy` 已足够 |
| R-5 | `NMP_SERVER_MODE` 默认 `debug` 被直接用于生产 → 弱集成凭据不被拒、cookie `Secure` 不开 | compose 注释红字 + 08 文档「生产必设 release」；**不能直接翻默认成 release**——release 下 `Validate` 无条件要求 netbox/glpi token，会让主链起不来（G-15 解耦后再翻） |
| R-6 | 网络别名 `backend` 让「服务名」有两个（`api` 与 `backend`），后来者困惑 | compose 内联注释说明「别名只为兼容 frontend/nginx.conf」；文档同一处写明 |
| R-7 | aux 加 `profiles` 后，原有「裸 `up -d` 拉起全栈」的用户行为改变 | 文档明写 profile 用法与原因；CHANGELOG 记一条；aux 本来也起不来（库未建/占位凭据），不是回退 |
| R-8 | `.gitignore` 删掉裸 `Dockerfile` 后，本机其它项目的临时 Dockerfile 可能被误提交 | 本仓库根目录无 Dockerfile，删除后由 `git status` 显式可见；如需忽略临时文件改用带路径的具体规则 |

## 6. 不做（划界）+ 新登记

**不做**：
- 不加 `database.automigrate` 开关、不做独立 migrate 服务（D-C 已论证，转 G-14）。
- 不钉基础镜像 digest（D-B 已论证）。
- 不改 aux 服务的集成语义（zabbix 用 `zabbix-server-pgsql` 无 Web/API，URL 指向本就不对，
  I-4；属集成模块）——本轮只做 profile 隔离。
- 不改端口暴露之外的网络架构（gRPC 50051 与 aux 同网，I-3，转 G-17）。
- 不在 CI 跑完整 compose 栈（D-F 已论证）。
- 不动 G-11/G-12（已单独登记）。
- 不删 `container_name`（C-F7）：`nmp-*` 是既有命名约定（smoke 脚本按前缀检测、文档/截图引用），
  且 compose 单实例语义已写进 §8.3；同机多 checkout 并存由 smoke 脚本 step 0 显式拒绝。

**新登记**：
| # | 内容 |
| --- | --- |
| G-14 | 迁移与运行时解耦：`database.automigrate` 开关（新键需 yaml 占位 + `SetDefault`）+ 独立 migrate 服务 + 多副本部署文档 |
| G-15 | release 校验解耦：`integrations.netbox.token` / `glpi.*_token` 改为「URL 配置了才校验」（zabbix 已是此模式），解耦后 compose 默认 `NMP_SERVER_MODE=release` 才可行 |
| G-16 | GORM logger 硬编码 `LogLevel: logger.Info`（`database.go:38-46`），release 也全量打印每条 SQL 到容器 stdout；应接到 `cfg.Log.Level` |
| G-17 | aux 服务生产化：独立 DB 角色/库、独立 network、版本标签钉死（`latest`→具体版本）、真实 secret、gRPC 端口隔离；另需 `cap_drop: [ALL]` + `no-new-privileges`（安全审计 P2：aux 与主链同网段，被攻陷容器可读到主库超级用户密码，且 L2 上可 ARP 冒充 `172.28.0.10` 成为「受信代理」） |
| G-18 | `database.Init` 的 gorm `AutoMigrate` 兜底在真实 postgres 上不可用（空库不建种子角色、已迁移库报 `insufficient arguments`）；三个 CLI 已改为注入 `MigrationsFS` 绕过，兜底本身待删/限定 |

## 7. 审查处置（逐条）

### 7.1 安全/运维视角

| # | 结论 | 处置 |
| --- | --- | --- |
| B-1 | `.gitignore` 吞 Dockerfile | **接受** → Where 首行 + V-6 |
| B-2 | 独立 migrate 服务不解决并发、api 本来就自迁移；建议加 `automigrate` 开关否则砍掉服务 | **部分接受**：砍掉 migrate 服务（D-C），**不加开关**（超范围，转 G-14）；理由与证据写入 D-C |
| B-3 | migrate 需全量 secret | **接受**（随 D-C 砍服务而消解） |
| B-4 | `compose config -q` 守卫范围虚高 | **接受** → D-F 收窄 + `test -f` + 说明分工 |
| B-5 | aux 共享 postgres 角色 + 占位凭据默认上线 | **接受** → aux 加 `profiles: ["aux"]`，DB 密码与主链同源；其余（独立角色/库）转 G-17 |
| B-6 | 端口暴露 | **接受** → postgres/redis 不发布，api 绑 `127.0.0.1`，web 为唯一入口；顺带解决宿主机 6379 冲突 |
| S-1 | `mode` 默认 debug 的后果核实 + 建议翻默认 | **部分接受**：核实结论写入 R-5；**不翻默认**（release 会因 netbox/glpi 校验拒启），转 G-15；GORM 日志问题转 G-16 |
| S-2 | G-13 定级 P1、D-A 先落 | **接受** → D-A 排为第一个提交 |
| S-3 | fresh DB 无用户，登录必 401 | **接受** → 镜像 build `admin-bootstrap`，D-H 建号后断言 200 |
| S-4 | `down -v` 误删 + 假绿 | **接受** → 随机项目名、`--destroy` 显式、`/readyz`、psql 断言 0 行判红 |
| S-5 | CI `-q` 与 `permissions` | **接受** → D-F |
| S-6 | 供应链 | **接受**：`npm ci` + lock 已足够；digest 不钉，再评估周期记入 TODO |
| S-7 | 非 root 取舍、`read_only`、busybox wget | **接受**：busybox wget 够用（不额外 apk）；`read_only: true` 待 D-H 跑通后加（避免首轮引入额外变量） |
| I-1 | 08 §8.3 与仓库 compose 漂移 | **接受** → D-G 对齐 |
| I-2 | `/metrics` 默认开放未鉴权 | 不在本轮；已记录（低敏，文档建议生产开） |
| I-3 | gRPC 与 aux 同网 | 转 G-17 |
| I-4 | zabbix URL 指向 server 而非 web | 不在本轮；D-G 文档注明 |

### 7.2 正确性视角

| # | 结论 | 处置 |
| --- | --- | --- |
| M-1 | `.gitignore` 吞 Dockerfile | **接受**（同 B-1） |
| M-2 | `ENTRYPOINT` + `command` 拼接 → migrate 起成 server | **接受** → D-B 改 `CMD`（服务已砍，但镜像语义仍要对） |
| M-3 | D-C 论证失真（api 无条件自迁移） | **接受** → D-C 重写，rev1 方案 B 作废 |
| M-4 | D-F 作用夸大 | **接受**（同 B-4） |
| S-1 | 补 `SetDefault("auth.api_key_pepper","")` | **接受** → D-A.3 + V-3 |
| S-2 | D-H 的 `172.28.0.1` 断言环境相关 | **接受** → 硬不变量 + 打印实际值 |
| I-1 | viper 机制判断准确 | 采用 |
| I-2 | 报错文案变量名 | 采用（D-A.2） |
| I-3 | 唯一 env 缺口是 pepper | 采用（缩小范围） |
| I-4 | CGO 结论 | 采用（D-B 注释改正） |
| I-5 | 镜像标签真实存在 | 采用 |
| I-6 | `version` 可删 | 采用 |
| I-7 | 网络别名方案正确 | 采用 |
| I-8 | 迁移幂等/可重入 | 采用（支撑 D-C） |
| I-9 | 裸 `up` 不会整栈健康 | **接受** → D-E profiles + D-G 文档显式服务清单 |
| I-10 | D-E env 逐项核对拼写正确 | 采用 |

### 7.3 交付后审计（2026-09-09，4 个独立视角）

四名审计员分别从**正确性/运行时**、**安全**、**文档漂移**、**变异反证**切入，
全部结论都带可复跑的命令或日志证据（smoke 日志 `/tmp/smoke-compose2.log`）。

| # | 发现（严重度） | 处置 |
| --- | --- | --- |
| C-F1 | `make db-reset` 调 `./cmd/seed --reset`，而 seed 无 flag 解析 → 提示「数据将丢失」却什么都没删（中） | **接受** → 改为 `migrate reset` → `migrate up` → `seed` 三步；help 文案同步 |
| C-F2 | make 的 `include .env` 把值里的 `#` 当注释截断（`ab#cd`→`ab`），compose 侧不截断 → 宿主迁移认证失败且报错不提截断（中） | **接受** → 删掉 `include/export`，改 recipe 内 `set -a; . ./.env; set +a`（shell 里 `#` 只在词首是注释） |
| C-F3 | api HEALTHCHECK `start-period=10s` 对「先迁移后监听」零余量，慢 IO 上 `--wait` 会硬失败（中低） | **接受** → `--start-period=180s --retries=20` |
| C-F4 | `make migrate-create` 调不存在的 `create` 子命令（低） | **接受** → 改为打印真实子命令与手工建文件步骤并 `exit 1` |
| C-F5 | `deploy`/`deploy-min` 的宿主 `db-migrate` 与 api 自迁移重复，锁竞争时会中止（低） | **接受** → 从两条链里去掉 `db-migrate`（`make db-migrate` 保留给手工用） |
| C-F6 | smoke 硬编码 `schema_migrations == 13`（低） | **接受** → 从迁移目录动态取 `*.up.sql` 条数 |
| C-F7 | 固定 `container_name` 与随机项目名隔离自相矛盾（低） | **不修**（`nmp-*` 是既有约定，smoke 脚本 step 0 已拒绝共存；单实例语义记入 §6 不做） |
| S-P0 | `backend/.env.example` 的占位 secret 能过 `Validate()`（长度合法、不在黑名单）→ 照抄即「已知密钥上线」（高） | **接受** → 三处改空值 + 注释说明为何不放示例值 |
| S-P1 | 默认 `debug` + `web` 绑 `0.0.0.0:3000` 明文，README 却把 `deploy-min` 标「生产」（高） | **接受** → web 改绑 `127.0.0.1:3000`；README/08 生产段补「`NMP_SERVER_MODE=release` + TLS 终结」两条必做项 |
| S-P2 | aux 与主链共用 postgres 超级用户密码 + 同一 L2 网段（可 ARP 冒充受信代理）（中） | **转 G-17**（补 `cap_drop`/独立库/独立 network 到待办） |
| S-P3 | `web` 以 root 运行、无 HEALTHCHECK（中） | HEALTHCHECK **本轮修**；非 root 运行 **转 G-19** |
| S-P4 | GORM `LogLevel: Info` 把 bcrypt/API Key 哈希打进容器日志（中） | **转 G-16**（补实测证据：`/tmp/smoke-compose2.log:184`） |
| S-P5/P6 | aux 占位凭据、浮动 tag（中/低） | **转 G-17**（已含） |
| D-M1..M5 | 08 章「Dockerfile 不存在」假陈述、TODO 端口/8 服务/24 target、CHANGELOG 缺条、V-15 证据不足（中） | **接受** → 逐条改文档；V-15 补 smoke 步骤 5b + CI 回归步 |
| D-M6..M11 / L1..L7 | plan 的 `--destroy` 与脚本相反、Makefile help/.PHONY、plan §6 漏 G-18、aux 端口列、`web healthy` 措辞、apikey 注释、CI job 数、Go 版本、测试数、deploy-status 覆盖（低） | **接受** → 逐条改（deploy-status 的 aux 只列有端口可探的 4 个，ES/Mongo 无宿主端口，见 §8.3.1） |
| V-E1/E2 | pepper 的 `SetDefault` 与报错文案**有**真实回归保护（变异反证通过） | 采用 |
| V-E3 | 删 `config.yaml` 的 `api_key_pepper: ""` 占位不会让任何测试变红（功能由 `SetDefault` 保证） | **如实记录**：该占位是文档/双保险，注释里「必需的」措辞已在 `config.yaml` 更正 |
| V-E4 | 三个 CLI 的 `SetMigrationsFS` **零自动化回归保护**（中，本轮最大缺口） | **接受** → CI `dbsmoke` 新增「CLI 迁移路径回归」步（真 PG 跑 admin-bootstrap/set-role/seed，去掉注入必红）+ smoke 步骤 5b 端到端复跑 |

**新登记（本轮）**：G-19（web 容器 root 运行）、G-20（`Asset.CustomFields` 零值 `''` 在真 PG 上是非法 JSON → 建资产/seed 资产全失败，实测证据见 TODO）。
