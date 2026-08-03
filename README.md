# NewSzxcn-Email

NewSzxcn-Email 是一个可自建、可管理、开箱即用的开源邮箱系统。

## 功能

- Webmail 收发邮件、写信、附件、草稿、搜索、星标、标签、已读/未读
- 多邮箱、多域名、DKIM、DNS 检测、邮件转发
- 账号管理、邮箱数量配额、权限配额、注册与自助申请邮箱
- 管理后台、全部邮件、发送队列、系统设置
- Postfix、Dovecot、Rspamd、SQLite、Docker 单容器部署

## 截图


  <img src="docs/screenshots/mail-preview.png" alt="邮箱首页" width="49%" />
  <img src="docs/screenshots/compose-preview.png" alt="写邮件" width="49%" />
  <img src="docs/screenshots/admin-preview.png" alt="后台管理" width="49%" />
  <img src="docs/screenshots/client-preview.png" alt="邮箱管理" width="49%" />


## 技术栈

- 后端：Go
- 前端：React + TypeScript + shadcn/ui
- 数据库：SQLite
- 邮件服务：Postfix + Dovecot + Rspamd
- 部署：Docker / Docker Compose

## 快速部署

```bash
cd deploy
cp .env.example .env
# 修改域名、访问地址、管理员邮箱、管理员密码
docker compose up -d --build
```

公网收发邮件需要配置：

- MX
- SPF
- DKIM
- DMARC
- 25 / 465 / 587 / 993 / 995 端口

## 本地开发

```bash
cd apps/api
go run ./cmd/server
```

```bash
cd apps/web
pnpm install
pnpm run dev
```

## 说明

这是 NewSzxcn 自用维护版本，后续功能和界面修改都以本仓库为准。

## License

[MIT](./LICENSE)
