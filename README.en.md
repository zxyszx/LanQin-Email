# NewSzxcn-Email

NewSzxcn-Email is a self-hosted, manageable, ready-to-run open-source email system.

Live site: [mail.newszxcn.com](https://mail.newszxcn.com)

## Features

- Webmail: inbox, compose, attachments, drafts, search, stars, labels, read/unread
- Multi-mailbox and multi-domain management, DKIM, DNS checks, forwarding
- Account management, mailbox quotas, permission quotas, registration, mailbox requests
- Admin console, all mail, send queue, system settings
- Postfix, Dovecot, Rspamd, SQLite, Docker single-container deployment

## Screenshots

Replace these images when needed:

- `docs/screenshots/mail-preview.png`
- `docs/screenshots/compose-preview.png`
- `docs/screenshots/admin-preview.png`
- `docs/screenshots/client-preview.png`

## Stack

- Backend: Go
- Frontend: React + TypeScript + shadcn/ui
- Database: SQLite
- Mail stack: Postfix + Dovecot + Rspamd
- Deployment: Docker / Docker Compose

## Quick Deploy

```bash
cd deploy
cp .env.example .env
# Edit domain, public URL, admin email, and admin password
docker compose up -d --build
```

Public mail delivery requires MX, SPF, DKIM, DMARC, and open mail ports.

## Note

This is the NewSzxcn maintained version. Future changes are based on this repository.

## License

[MIT](./LICENSE)
