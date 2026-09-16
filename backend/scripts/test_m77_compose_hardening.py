"""M77 容器级硬化断言（G-19 web 容器最小权限）。

为什么有这张测试：M77 / G-19 钉死 6 项 compose 硬化（read_only / tmpfs / cap_drop /
security_opt / 端口 / 静态 IP）。compose 不强制这些（仅解析），必须脚本断言才能
阻止「下次改 compose 时漏改」漂移。运行：

    python3 -m pytest backend/scripts/test_m77_compose_hardening.py -v

或：

    python3 backend/scripts/test_m77_compose_hardening.py
"""
import os
import sys
from pathlib import Path

try:
    import yaml
except ImportError:
    print('PyYAML required (apt: python3-yaml / pip: pyyaml)')
    sys.exit(1)

REPO_ROOT = Path(__file__).resolve().parent.parent.parent
COMPOSE = REPO_ROOT / 'docker-compose.yml'


def _load_compose():
    """加载 docker-compose.yml，NMP_* secret 占位（避免 ${VAR:?} 报错）"""
    import tempfile
    import subprocess

    env = os.environ.copy()
    env.update({
        'NMP_DATABASE_PASSWORD': 'test-pwd',
        'NMP_AUTH_JWT_SECRET': 'test-jwt-secret',
        'NMP_AUTH_API_KEY_PEPPER': 'test-pepper',
    })
    r = subprocess.run(
        ['docker', 'compose', '-f', str(COMPOSE), 'config'],
        capture_output=True, text=True, cwd=str(REPO_ROOT), env=env,
    )
    if r.returncode != 0:
        raise RuntimeError(f'compose config failed: {r.stderr[-500:]}')
    return yaml.safe_load(r.stdout)


def test_web_read_only():
    """G-19 / M77：read_only: true —— 容器 rootfs 只读，写入仅允许 tmpfs"""
    data = _load_compose()
    web = data['services']['web']
    assert web.get('read_only') is True, \
        f'read_only must be True (G-19), got {web.get("read_only")}'


def test_web_cap_drop_all():
    """G-19 / M77：cap_drop: [ALL] —— 砍掉所有 Linux capabilities"""
    data = _load_compose()
    web = data['services']['web']
    cap_drop = web.get('cap_drop', [])
    assert 'ALL' in cap_drop, \
        f'cap_drop must include ALL (G-19), got {cap_drop}'


def test_web_no_new_privileges():
    """G-19 / M77：security_opt: [no-new-privileges:true] —— 防 setuid 提权"""
    data = _load_compose()
    web = data['services']['web']
    sec = web.get('security_opt', [])
    assert 'no-new-privileges:true' in sec, \
        f'security_opt must include no-new-privileges:true (G-19), got {sec}'


def test_web_tmpfs_three():
    """G-19 / M77：tmpfs 3 个 —— /var/cache/nginx /var/run /tmp"""
    data = _load_compose()
    web = data['services']['web']
    tmpfs = web.get('tmpfs', [])
    paths = [t.split(':')[0] for t in tmpfs]
    for required in ['/var/cache/nginx', '/var/run', '/tmp']:
        assert required in paths, \
            f'tmpfs must include {required} (G-19), got {tmpfs}'


def test_web_port_8080():
    """G-19 / M77：端口映射 127.0.0.1:3000:8080 —— unprivileged 默认端口"""
    data = _load_compose()
    web = data['services']['web']
    ports = web.get('ports', [])
    # ports 经 compose config 后变成 list of dict
    has_3000_8080 = False
    for p in ports:
        if isinstance(p, dict):
            target = p.get('target')
            published = str(p.get('published'))
            host_ip = p.get('host_ip', '')
            if target == 8080 and published == '3000' and host_ip == '127.0.0.1':
                has_3000_8080 = True
        elif isinstance(p, str):
            if '127.0.0.1:3000:8080' in p:
                has_3000_8080 = True
    assert has_3000_8080, \
        f'ports must include 127.0.0.1:3000:8080 (G-19 unprivileged), got {ports}'


def test_web_static_ip_preserved():
    """G-7：静态 IP 172.28.0.10 不动（G-19 不破 G-7）"""
    data = _load_compose()
    web = data['services']['web']
    networks = web.get('networks', {})
    default = networks.get('default', {})
    ipv4 = default.get('ipv4_address')
    assert ipv4 == '172.28.0.10', \
        f'web 静态 IP 必须保留 172.28.0.10（G-7 不破）, got {ipv4}'


def test_dockerfile_unprivileged_base():
    """G-19 / M77：Dockerfile 用 nginxinc/nginx-unprivileged —— 不再 root"""
    dockerfile = (REPO_ROOT / 'frontend' / 'Dockerfile').read_text()
    assert 'FROM nginxinc/nginx-unprivileged' in dockerfile, \
        'Dockerfile 必须用 nginxinc/nginx-unprivileged（G-19）'
    # 反证：旧的裸 FROM nginx:1.27-alpine（root 镜像）不能残留
    assert not any(
        line.startswith('FROM ') and 'nginxinc/' not in line and 'nginx:' in line
        for line in dockerfile.split('\n')
    ), '旧 root FROM nginx:1.27-alpine 不应残留（G-19 必须切走）'


def test_nginx_conf_listen_8080():
    """G-19 / M77：nginx.conf listen 8080（unprivileged 默认端口）"""
    nginx = (REPO_ROOT / 'frontend' / 'nginx.conf').read_text()
    assert 'listen 8080' in nginx, \
        'nginx.conf 必须 listen 8080（G-19 unprivileged）'


if __name__ == '__main__':
    # Standalone runner
    failures = []
    tests = [
        ('test_web_read_only', test_web_read_only),
        ('test_web_cap_drop_all', test_web_cap_drop_all),
        ('test_web_no_new_privileges', test_web_no_new_privileges),
        ('test_web_tmpfs_three', test_web_tmpfs_three),
        ('test_web_port_8080', test_web_port_8080),
        ('test_web_static_ip_preserved', test_web_static_ip_preserved),
        ('test_dockerfile_unprivileged_base', test_dockerfile_unprivileged_base),
        ('test_nginx_conf_listen_8080', test_nginx_conf_listen_8080),
    ]
    for name, fn in tests:
        try:
            fn()
            print(f'✓ {name}')
        except AssertionError as e:
            print(f'✗ {name}: {e}')
            failures.append(name)
    if failures:
        print(f'\n{len(failures)}/{len(tests)} FAILED: {failures}')
        sys.exit(1)
    print(f'\n{len(tests)}/{len(tests)} PASS')
