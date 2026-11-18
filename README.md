# Dudle-NG

A from-scratch rewrite of [dudle](https://github.com/kellerben/dudle): a self-hosted scheduling poll tool.
Single static Go binary, SQLite storage, no external services, no tracking.

## Features
- Date polls (calendar picker, optional times) and free-choice polls
- Yes / "if need be" / no answers, best option highlighted automatically
- No accounts: participants get a link, the creator gets a private admin link
- Admin page: edit settings, add/remove options, remove participants/comments, close or delete the poll
- Comments, CSV export, Atom feed
- Light/dark mode, responsive, works without JavaScript (progressive enhancement)
- English and German (add a language in `i18n.go`)
- Inactive polls are deleted automatically (default: 365 days)

## Run
```sh
go run .                       # http://localhost:8080, data in ./dudle.db
go test ./...
```

Configuration (flag or environment variable):

| Flag | Env | Default | |
|---|---|---|---|
| `-addr` | `DUDLE_ADDR` | `:8080` | listen address |
| `-db` | `DUDLE_DB` | `dudle.db` | SQLite file |
| `-base-url` | `DUDLE_BASE_URL` | derived from request | public URL for share links/feeds |
| `-expire-days` | `DUDLE_EXPIRE_DAYS` | `365` | delete polls inactive this long, `0` = never |
| `-base-path` | `DUDLE_BASE_PATH` | none | serve under a URL prefix such as `/dudle`; the reverse proxy forwards the path unchanged. If you set `-base-url`, include the prefix there |
| `-contact` | `DUDLE_CONTACT` | none | email or URL for accessibility feedback, shown on `/accessibility`. **Set this** for a public instance |
| `-imprint-url` | `DUDLE_IMPRINT_URL` | none | link to the legal notice (Impressum) in the footer |
| `-privacy-url` | `DUDLE_PRIVACY_URL` | none | link to the privacy policy in the footer |
| `-rate-limit` | `DUDLE_RATE_LIMIT` | `60` | max write requests (POST) per client per minute, `0` = off |
| `-trust-proxy` | `DUDLE_TRUST_PROXY=1` | off | identify clients by `X-Forwarded-For`; only enable behind a reverse proxy that sets it |

Docker:
```sh
docker build -t dudle-ng . && docker run -p 8080:8080 -v dudle-data:/data dudle-ng
```
Run it behind a TLS-terminating reverse proxy (it honours `X-Forwarded-Proto`).

## Layout
- `store.go` – SQLite schema and data access
- `logic.go` – validation, parsing, vote summary
- `handlers.go` – HTTP routes and view models
- `i18n.go` – translations
- `web/` – embedded templates, CSS and JS

## Access control and participants
- Optional **participant password** (visitors must unlock the poll to see, vote and export) and optional **admin password** (required in addition to the secret admin link). Set both on the admin page.
- Passwords are stored as salted PBKDF2-SHA256 hashes; unlocking sets a signed cookie that becomes invalid when the password changes. Password attempts are limited to 10 per minute per client and poll.
- The admin can **invite participants** by name; anyone can edit or delete (with confirmation) a participant on the poll page while the poll is open.

## Accessibility
Target: WCAG 2.1 level AA (EN 301 549, the basis of BITV 2.0 and the German BFSG). The footer links to an accessibility statement at `/accessibility` (German and English). It is based on a self-assessment (automated axe-core checks, keyboard-only and 320 px checks) and says so: no external audit and no screen reader tests yet. Set `DUDLE_CONTACT` so the statement names a feedback contact, and review the statement text for your situation (whether the BFSG applies to you is for you or your legal counsel to decide).

## Privacy facts (for the data protection officer)
- Stored per poll: title, description, options, participant names and answers, comments, optional password hashes. Polls are deleted after `-expire-days` (default 365) without activity, or by the organiser at any time.
- Cookies (all strictly necessary, no tracking): `lang` (chosen language), `me_<poll>` (proves which entries this browser created, so only their author can change them), `mgr_<poll>` (this browser manages the poll), `pw_*` (unlocked password-protected poll).
- Server log: method, path and duration; admin secrets in paths are redacted. The bundled nginx snippet turns off the access log for `/dudle/` because URLs contain admin links.
- No third-party requests, fonts, analytics or CDNs.

## Who can change entries
Only the browser that created an entry, and the organiser (a browser that opened the admin page), can edit or delete it. Entries invited by the organiser, and entries created before this rule existed, can be claimed once by whoever fills them in first.

## Known limitations
- Rate limiting is per client IP and in memory (resets on restart); it only covers write requests.
- Anyone with a poll link can edit any participant's answers (same as classic dudle/Doodle).
- Poll and admin links are secrets; losing the admin link means losing admin access.

## License

Licensed under the [GNU Affero General Public License v3.0](License) (AGPL-3.0-or-later), the same license as the original [dudle](https://github.com/kellerben/dudle).

If you run a modified version as a network service, section 13 of the AGPL requires that you offer your users the corresponding source code, for example with a link to your repository.
