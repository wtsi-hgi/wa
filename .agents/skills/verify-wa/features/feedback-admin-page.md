# Feedback Admin Page

## What It Is

The server admin reads agent feedback at frontend `/feedback` ("Agent
feedback"). It lists reports newest first, filters by "Show acknowledged"
and Category, pages 50 at a time, and can Acknowledge, Unacknowledge, or
Delete each report. Next.js reads the MLWH admin token from disk on the
server side and calls `${WA_MLWH_BACKEND_URL}/feedback`. The browser never
sees the token.

## How To Reach It

- Account menu: log in (see the skill's Login), open the `<user> account`
  button, then the `Feedback` menu item. The item shows only for admins, who
  are users in `WA_FEEDBACK_ADMINS` or the OS user running Next.js.
- Direct URL: `$FRONTEND_URL/feedback`, with query `show=all` and
  `category=<enum>`.

## How To Drive It

`node $SKILL/scripts/drive-feedback.mjs "$RUN"` (exit 0, `ALL PASS`) runs
the whole path after submitting a fresh report:

1. Open `$FRONTEND_URL`, click `Log in`, fill `input[name=username]` and
   `input[name=password]` in `form[aria-label="Log in"]`, click `Continue`.
   Expect the `<user> account` button. Evidence: `03-login-form.png`.
2. Open the account menu. Expect menuitem `Feedback`.
   Evidence: `04-account-menu.png`.
3. Click it. Expect heading `Agent feedback`, an `N report(s)` count, and
   `article[aria-label="Feedback #<id>"]` with the category badge,
   description, `User request`, `Tools tried`, and a metadata line
   (`Client … · MCP server … · wa API … · Transport … · From 127.0.0.1`).
   Evidence: `05-feedback-page.png` (full page), `06-feedback-report.png`
   and `.txt` (the card).
4. Click the card's `Acknowledge`. Expect the card to leave the default
   (unacknowledged) view. Click the `Show acknowledged` checkbox. Expect the
   URL to gain `show=all` and the card to return with an `Acknowledged`
   badge and an `Unacknowledge` button. Evidence:
   `07-acknowledged-report.png`. Side effect: admin API shows
   `acknowledged:true` with `acknowledged_at`, in
   `08-admin-list-after-ack.json`.

Delete is not in the script. To drive it, click `Delete` on a card and
accept the `window.confirm` dialog (`page.once("dialog", d => d.accept())`).
Expect the card gone, and `GET /feedback` no longer lists the id.

## Gotchas

- Non-OK states render one `role="status"` panel instead of the list.
  "Log in to view feedback." means no session. "You do not have access to
  feedback." means not an admin. "Feedback is unavailable." comes with a
  reason: token unreadable (Next.js has a different user or
  `XDG_STATE_HOME` from serve), disabled on the MLWH server, token rejected,
  server too old, or unreachable.
- run-dev test mode gives every child `XDG_STATE_HOME=<repo>/.tmp/state-test`,
  so the token is shared. A hand-started Next.js needs the same value.
- The `Show acknowledged` checkbox is controlled by the URL. Click it and
  wait for the URL. Playwright's `check()` fails because the state flips
  only after navigation.
- Report ids keep counting within one launch. Find the card by the id the
  POST returned, never by position.
- `next dev` compiles each route on first hit. The first `/feedback` load can
  take several seconds.
