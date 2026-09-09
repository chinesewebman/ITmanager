#!/usr/bin/env bash
#
# smoke-compose.sh — 在真实 Docker 上跑通主链，并端到端验证 G-7（XFF 可信边界）。
#
# 为什么需要它：CI 不 build/up 完整栈（成本），单测用的是 httptest 伪造的 RemoteAddr。
# 这里跑的是真实 nginx → 真实容器网络 → 真实 ClientIP() → 真实审计落库。
# 方案与验证项：docs/FIX-PLAN-COMPOSE-RUNTIME.md（D-H / V-8..V-11、V-15）。
#
# 用法：
#   scripts/smoke-compose.sh            # 跑完自动清理（含卷与本地镜像）
#   scripts/smoke-compose.sh --keep     # 保留容器/卷，便于排障（打印项目名）
#
# 安全：全程使用**随机 COMPOSE_PROJECT_NAME**，只操作本项目自己的资源；
# 若检测到已存在 nmp-* 容器（可能是别人的真实部署），直接拒绝执行。
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
COMPOSE_FILE="$REPO_ROOT/docker-compose.yml"
PROJECT="itmanager-smoke-$$-$RANDOM"
KEEP=0
[[ "${1:-}" == "--keep" ]] && KEEP=1

WORK="$(mktemp -d)"
ENV_FILE="$WORK/smoke.env"

dc() { docker compose --project-name "$PROJECT" --env-file "$ENV_FILE" -f "$COMPOSE_FILE" "$@"; }

# 退出时回收本项目资源（含被 Ctrl-C / 中断的情况），避免残留网络占住固定子网。
CLEANED=0
cleanup() {
  [[ "$KEEP" == "1" || "$CLEANED" == "1" ]] && return 0
  CLEANED=1
  dc down -v --rmi local >/dev/null 2>&1 || true
}
trap 'cleanup; rm -rf "$WORK"' EXIT
trap 'exit 130' INT TERM   # 信号 → 退出 → 由 EXIT trap 回收资源

fail() { echo "❌ $*" >&2; exit 1; }
ok()   { echo "✅ $*"; }
step() { echo; echo "── $* ──"; }

# 库里跑 psql；第二个参数可指定库名（默认 network_monitor）
psql_q() { dc exec -T postgres psql -U nmp -d "${2:-network_monitor}" -tAc "$1" | tr -d '\r'; }

step "0. 前置检查"
docker compose version >/dev/null || fail "需要 docker compose v2"
if docker ps -a --format '{{.Names}}' | grep -q '^nmp-'; then
  fail "检测到已存在的 nmp-* 容器（真实部署，或上一次被中断的冒烟）。
       先确认归属：docker ps -a --filter name=^nmp-
       确认是冒烟残留后再清理：docker compose --project-name <上次的项目名> \\
         --env-file <任意含三个 secret 的 env> -f docker-compose.yml down -v --rmi local"
fi
# 被 Ctrl-C / kill 的冒烟会留下**空**网络占住固定子网（compose 固定用 172.28.0.0/24），
# 让本次 up 在 "Network ... Creating" 阶段直接失败（实测踩过）。只删名字属于本脚本、
# 且当前没有任何容器连接的网络，绝不碰别人的网络。
for n in $(docker network ls --filter name='^itmanager-smoke-' --format '{{.Name}}'); do
  [[ "$(docker network inspect "$n" --format '{{len .Containers}}')" == "0" ]] || continue
  docker network rm "$n" >/dev/null && echo "   （清理上次残留的空网络 $n）"
done
ok "docker compose $(docker compose version --short)，项目名 $PROJECT"

step "1. 生成临时 .env（随机 secret）"
DB_PW="$(openssl rand -hex 24)"
JWT="$(openssl rand -hex 32)"
PEPPER="$(openssl rand -hex 32)"
ADMIN_PW="Smoke-$(openssl rand -hex 12)"
cat > "$ENV_FILE" <<EOF
NMP_DATABASE_PASSWORD=$DB_PW
NMP_AUTH_JWT_SECRET=$JWT
NMP_AUTH_API_KEY_PEPPER=$PEPPER
NMP_SERVER_MODE=debug
EOF
chmod 600 "$ENV_FILE"
ok ".env 已生成（$WORK）"

step "2. 拉起主链（postgres → redis → api → web）"
dc up -d --build --wait --wait-timeout 900 postgres redis api web \
  || { dc logs --no-log-prefix api | tail -40; fail "compose up 失败（上方为 api 日志）"; }
ok "四个服务就绪"

step "3. /readyz（真 ping DB）"
for i in $(seq 1 60); do
  code="$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/readyz || true)"
  [[ "$code" == "200" ]] && break
  sleep 2
done
[[ "$code" == "200" ]] || fail "/readyz 未就绪（最后状态码 $code）"
ok "/readyz == 200"

step "4. 迁移真的跑过（schema_migrations）"
# 期望条数从迁移目录动态取，避免新增 000014 后这里变成假红
want="$(find "$REPO_ROOT/backend/migrations" -maxdepth 1 -name '*.up.sql' | wc -l | tr -d ' ')"
n="$(psql_q 'select count(*) from schema_migrations')"
[[ "$n" == "$want" ]] || fail "schema_migrations 期望 $want 条，实际 '$n'（迁移没跑或版本漂移）"
ok "schema_migrations = $n 条（= 迁移文件数）"

step "5. 建首个管理员（空库默认无任何用户）"
dc exec -T \
  -e FIRST_ADMIN_USERNAME=smokeadmin \
  -e FIRST_ADMIN_PASSWORD="$ADMIN_PW" \
  api ./admin-bootstrap >/dev/null || fail "admin-bootstrap 失败"
ok "已创建 smokeadmin"

step "5b. 三个 CLI 在真库里可用（D-I 回归：MigrationsFS 注入）"
# set-role 需要第二个 admin，否则触发「唯一管理员不许降级」守卫
dc exec -T -e FIRST_ADMIN_USERNAME=smokeadmin2 -e FIRST_ADMIN_PASSWORD="$ADMIN_PW" \
  api ./admin-bootstrap >/dev/null || fail "第二个 admin 创建失败"
dc exec -T -e SET_ROLE_USERNAME=smokeadmin -e SET_ROLE_ROLE=auditor \
  api ./set-role >/dev/null || fail "set-role 失败"
n="$(psql_q "select count(*) from user_roles ur join roles r on r.id=ur.role_id where r.code='auditor'")"
[[ "$n" == "1" ]] || fail "set-role 后 user_roles 未同步为 auditor（实际 '$n'）"
ok "set-role 生效（user_roles=auditor）"

# seed 在独立库上跑：主库已有用户，seed 会跳过用户但刷资产错误（G-20 既存缺陷）
dc exec -T postgres createdb -U nmp itmanager_cli_seed
dc exec -T -e NMP_DATABASE_NAME=itmanager_cli_seed api ./seed >/dev/null || fail "seed 失败"
n="$(psql_q "select count(*) from users where username='admin'" itmanager_cli_seed)"
[[ "$n" == "1" ]] || fail "seed 未建出 admin（实际 '$n'）"
ok "seed 生效（独立库 itmanager_cli_seed）"

step "6. 经 nginx 登录（伪造 XFF）"
login() {
  curl -sS -o /dev/null -w '%{http_code}' \
    -H 'Content-Type: application/json' \
    -H 'X-Forwarded-For: 1.2.3.4' \
    -d "{\"username\":\"smokeadmin\",\"password\":\"$1\"}" \
    http://localhost:3000/api/auth/login
}
code="$(login "$ADMIN_PW")"
[[ "$code" == "200" ]] || fail "经 nginx 的登录期望 200，实际 $code（链路或凭据有问题，不能放行）"
ok "登录成功（200）"

step "7. 审计 IP：伪造的 XFF 必须未被采信（G-7 端到端）"
audit_ip() { psql_q "select ip from audit_logs where path='/api/auth/login' order by created_at desc limit 1"; }
ip="$(audit_ip)"
[[ -n "$ip" ]] || fail "audit_logs 没有登录记录（审计链路断了）"
[[ "$ip" != "1.2.3.4" ]] || fail "审计 IP 采信了伪造的 XFF！G-7 回归"
[[ "$ip" != "172.28.0.10" ]] || fail "审计 IP 是 nginx 容器地址，说明受信代理没生效（应为真实客户端）"
ok "审计 IP = $ip（≠ 伪造值 1.2.3.4，≠ 代理地址 172.28.0.10）"
echo "   注：默认 docker bridge NAT + 经 127.0.0.1 连入时该值 == 网关 172.28.0.1；"
echo "   rootless / 关 userland-proxy / 非环回地址连入时实际值会变，故只断言不变量。"

step "8. 负例：把受信代理配错 → ClientIP() 退化为直连对端（nginx）"
printf 'NMP_SERVER_TRUSTED_PROXIES=1.2.3.4\n' >> "$ENV_FILE"
dc up -d api >/dev/null
for i in $(seq 1 60); do
  code="$(curl -s -o /dev/null -w '%{http_code}' http://127.0.0.1:8080/readyz || true)"
  [[ "$code" == "200" ]] && break
  sleep 2
done
sleep 2  # 等 api 接客
code="$(login "$ADMIN_PW")"
[[ "$code" == "200" ]] || fail "负例登录期望 200，实际 $code"
ip2="$(audit_ip)"
[[ "$ip2" == "172.28.0.10" ]] || fail "配错后审计 IP 期望退化为 nginx 容器地址 172.28.0.10，实际 '$ip2'"
ok "配错后审计 IP = $ip2（退化为直连对端，符合预期）"

step "9. 非 root 运行"
uid="$(dc exec -T api id -u | tr -d '\r')"
[[ "$uid" != "0" ]] || fail "容器内进程是 root（期望 uid 10001）"
ok "api 容器 uid = $uid"

if [[ "$KEEP" == "1" ]]; then
  echo
  echo "ℹ️  --keep：保留项目 $PROJECT（清理：docker compose --project-name $PROJECT -f $COMPOSE_FILE down -v --rmi local）"
else
  step "10. 清理（只删本项目的卷与本地镜像）"
  cleanup
  ok "已清理"
fi

echo
echo "🎉 compose 主链 + G-7 端到端全部通过"
