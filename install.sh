#!/usr/bin/env bash
set -Eeuo pipefail

REPOSITORY="zxyszx/NewSzxcn-Email"
RAW_BASE="https://raw.githubusercontent.com/${REPOSITORY}/main"
INSTALL_DIR="${LANQIN_INSTALL_DIR:-/opt/newszxcn-email}"
COMMAND="${1:-install}"
ROLLBACK_FILE="${INSTALL_DIR}/.rollback-image"

log() { printf '\033[1;34m[NewSzxcn]\033[0m %s\n' "$*"; }
success() { printf '\033[1;32m[完成]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[提示]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[错误]\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
NewSzxcn Email 管理命令

用法：newszxcn-email <command>

  install     首次安装或修复部署
  update      备份数据库并更新到最新版
  status      查看容器与健康状态
  logs        持续查看运行日志
  rollback    回滚到上次命令行更新前的镜像
  uninstall   停止并移除容器，保留邮件与配置
EOF
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    fail "请使用 root 运行，例如：curl -fsSL ${RAW_BASE}/install.sh | sudo bash"
  fi
}

require_curl() {
  command -v curl >/dev/null 2>&1 || fail "系统缺少 curl，请先安装 curl。"
}

ensure_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    log "未检测到 Docker，正在安装 Docker Engine..."
    curl -fsSL https://get.docker.com | sh
  fi
  if command -v systemctl >/dev/null 2>&1; then
    systemctl enable --now docker >/dev/null 2>&1 || true
  fi
  docker compose version >/dev/null 2>&1 || fail "需要 Docker Compose v2。"
}

compose() {
  docker compose --project-directory "${INSTALL_DIR}" -f "${INSTALL_DIR}/docker-compose.yml" "$@"
}

script_dir() {
  cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd
}

refresh_assets() {
  local source_dir
  source_dir="$(script_dir || true)"
  install -d -m 0755 "${INSTALL_DIR}"
  if [[ -f "${source_dir}/deploy/docker-compose.yml" && -f "${source_dir}/deploy/.env.example" ]]; then
    install -m 0644 "${source_dir}/deploy/docker-compose.yml" "${INSTALL_DIR}/docker-compose.yml"
    install -m 0644 "${source_dir}/deploy/.env.example" "${INSTALL_DIR}/.env.example"
    install -m 0755 "${source_dir}/install.sh" /usr/local/bin/newszxcn-email
  else
    curl -fsSL "${RAW_BASE}/deploy/docker-compose.yml" -o "${INSTALL_DIR}/docker-compose.yml"
    curl -fsSL "${RAW_BASE}/deploy/.env.example" -o "${INSTALL_DIR}/.env.example"
    curl -fsSL "${RAW_BASE}/install.sh" -o /usr/local/bin/newszxcn-email.new
    chmod 0755 /usr/local/bin/newszxcn-email.new
    mv /usr/local/bin/newszxcn-email.new /usr/local/bin/newszxcn-email
  fi
}

random_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
  else
    od -An -N24 -tx1 /dev/urandom | tr -d ' \n'
  fi
}

set_env() {
  local key="$1" value="$2" file="${INSTALL_DIR}/.env" tmp
  tmp="$(mktemp)"
  awk -v key="${key}" -v value="${value}" '
    BEGIN { found=0 }
    $0 ~ "^" key "=" { print key "=" value; found=1; next }
    { print }
    END { if (!found) print key "=" value }
  ' "${file}" > "${tmp}"
  cat "${tmp}" > "${file}"
  rm -f "${tmp}"
}

env_value() {
  local key="$1"
  sed -n "s/^${key}=//p" "${INSTALL_DIR}/.env" | tail -n 1
}

prompt_value() {
  local variable="$1" prompt="$2" default_value="$3" secret="${4:-false}"
  local value="${!variable:-}"
  if [[ -z "${value}" && -r /dev/tty ]]; then
    if [[ "${secret}" == "true" ]]; then
      read -r -s -p "${prompt}${default_value:+ [自动生成]}: " value </dev/tty
      printf '\n' >/dev/tty
    else
      read -r -p "${prompt}${default_value:+ [${default_value}]}: " value </dev/tty
    fi
  fi
  value="${value:-${default_value}}"
  printf '%s' "${value}"
}

configure_first_install() {
  if [[ -f "${INSTALL_DIR}/.env" ]]; then
    return
  fi
  install -m 0600 "${INSTALL_DIR}/.env.example" "${INSTALL_DIR}/.env"

  local hostname public_url admin_email admin_password update_token
  hostname="$(prompt_value LANQIN_PUBLIC_HOSTNAME "邮件服务器域名，例如 mail.example.com" "")"
  [[ "${hostname}" =~ ^[A-Za-z0-9.-]+\.[A-Za-z]{2,}$ ]] || fail "邮件服务器域名格式不正确。"
  public_url="$(prompt_value LANQIN_PUBLIC_BASE_URL "Webmail 访问地址" "https://${hostname}")"
  admin_email="$(prompt_value LANQIN_ADMIN_EMAIL "初始管理员邮箱" "admin@${hostname#mail.}")"
  [[ "${admin_email}" == *@*.* ]] || fail "管理员邮箱格式不正确。"
  admin_password="$(prompt_value LANQIN_ADMIN_PASSWORD "初始管理员密码" "" true)"
  if [[ -z "${admin_password}" ]]; then
    admin_password="$(random_secret)"
    warn "已自动生成管理员密码：${admin_password}"
  fi
  [[ ${#admin_password} -ge 10 ]] || fail "管理员密码至少需要 10 个字符。"
  update_token="$(random_secret)"

  set_env LANQIN_PUBLIC_HOSTNAME "${hostname}"
  set_env LANQIN_PUBLIC_BASE_URL "${public_url}"
  set_env LANQIN_ADMIN_EMAIL "${admin_email}"
  set_env LANQIN_ADMIN_PASSWORD "${admin_password}"
  set_env LANQIN_UPDATE_TOKEN "${update_token}"
  chmod 0600 "${INSTALL_DIR}/.env"
}

ensure_update_token() {
  local token
  token="$(env_value LANQIN_UPDATE_TOKEN || true)"
  if [[ -z "${token}" ]]; then
    set_env LANQIN_UPDATE_TOKEN "$(random_secret)"
    chmod 0600 "${INSTALL_DIR}/.env"
  fi
}

prepare_directories() {
  install -d -m 0755 "${INSTALL_DIR}/data" "${INSTALL_DIR}/mail" "${INSTALL_DIR}/dkim"
  install -d -m 0700 "${INSTALL_DIR}/data/backups"
}

wait_for_health() {
  local attempts="${1:-60}" bind port
  bind="$(env_value LANQIN_HTTP_BIND || true)"
  bind="${bind:-80}"
  port="${bind##*:}"
  for ((i=1; i<=attempts; i++)); do
    if curl -fsS --max-time 3 "http://127.0.0.1:${port}/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

backup_database() {
  local timestamp
  timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
  if [[ -n "$(compose ps -q lanqin-email 2>/dev/null || true)" ]]; then
    compose exec -T lanqin-email sh -c "mkdir -p /data/backups && sqlite3 /data/lanqin.db \".backup '/data/backups/cli-update-${timestamp}.db'\"" >/dev/null
    log "数据库已备份到 data/backups/cli-update-${timestamp}.db"
  fi
}

remember_current_image() {
  local container_id image_id rollback_tag
  container_id="$(compose ps -q lanqin-email 2>/dev/null || true)"
  [[ -n "${container_id}" ]] || return 0
  image_id="$(docker inspect --format '{{.Image}}' "${container_id}")"
  rollback_tag="newszxcn-email:rollback-$(date -u +%Y%m%d%H%M%S)"
  docker image tag "${image_id}" "${rollback_tag}"
  printf '%s\n' "${rollback_tag}" > "${ROLLBACK_FILE}"
}

do_install() {
  ensure_docker
  refresh_assets
  configure_first_install
  ensure_update_token
  prepare_directories
  log "正在拉取 NewSzxcn Email 镜像..."
  compose pull
  log "正在启动服务..."
  compose up -d --remove-orphans
  wait_for_health 90 || fail "服务未能通过健康检查，请执行 newszxcn-email logs 查看日志。"
  success "安装完成：$(env_value LANQIN_PUBLIC_BASE_URL)"
  warn "下一步请配置 MX、SPF、DKIM、DMARC，并确认 25/465/587/993/995 端口可访问。"
}

do_update() {
  [[ -f "${INSTALL_DIR}/.env" ]] || fail "尚未安装，请先执行 install。"
  ensure_docker
  refresh_assets
  ensure_update_token
  backup_database
  remember_current_image
  log "正在拉取最新版..."
  compose pull
  if ! compose up -d --remove-orphans; then
    warn "新版本容器启动失败，正在自动回滚。"
    do_rollback
    fail "更新失败，已回滚到原镜像。"
  fi
  if ! wait_for_health 90; then
    warn "新版本健康检查失败，正在自动回滚。"
    do_rollback
    fail "更新失败，已回滚到原镜像。"
  fi
  success "系统已更新，配置、邮件和数据库均已保留。"
}

do_rollback() {
  [[ -f "${ROLLBACK_FILE}" ]] || fail "没有可用的回滚镜像。"
  local image
  image="$(tr -d '\r\n' < "${ROLLBACK_FILE}")"
  docker image inspect "${image}" >/dev/null 2>&1 || fail "回滚镜像已不存在：${image}"
  log "正在回滚到 ${image}..."
  LANQIN_IMAGE="${image}" compose up -d --no-deps --force-recreate lanqin-email
  wait_for_health 90 || fail "回滚后服务仍未通过健康检查，请查看日志。"
  success "已回滚到 ${image}。"
}

do_status() {
  [[ -f "${INSTALL_DIR}/docker-compose.yml" ]] || fail "尚未安装。"
  compose ps
  if wait_for_health 1; then
    success "Web 与 API 健康检查正常。"
  else
    fail "健康检查失败。"
  fi
}

do_uninstall() {
  [[ -f "${INSTALL_DIR}/docker-compose.yml" ]] || fail "尚未安装。"
  compose down --remove-orphans
  success "容器已移除，${INSTALL_DIR} 中的配置、邮件和数据库仍然保留。"
}

require_root
require_curl
case "${COMMAND}" in
  install) do_install ;;
  update) do_update ;;
  status) ensure_docker; do_status ;;
  logs) ensure_docker; compose logs -f --tail=200 lanqin-email updater ;;
  rollback) ensure_docker; do_rollback ;;
  uninstall) ensure_docker; do_uninstall ;;
  help|-h|--help) usage ;;
  *) usage; fail "未知命令：${COMMAND}" ;;
esac
