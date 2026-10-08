# internal/mailsender

The SMTP mail sender of this project, built from the typed configuration. It is
opt-in and explicit: `weld add mail` installs the sender, it connects mail to
nothing. Nothing in the generated application imports this package, so building,
testing and serving send no mail and need no SMTP server.

## Sending is one call

```go
sender, err := mailsender.New(cfg) // validates the mail section; no connection
if err != nil {
    return err
}
err = sender.Send(ctx, mailsender.Message{
    To:      []string{"user@example.com"},
    Subject: "Hello",
    Text:    "plain text body",
    HTML:    "<p>HTML body</p>",
})
```

`New` validates the `mail` section and stores the settings. It sends nothing and
opens no connection. Every `Send` dials the configured server, applies the
configured TLS policy, authenticates when credentials are set, submits the
message and quits. There is no connection pool, no background goroutine, no
queue and no outbox; `ctx` and the configured `timeout_seconds` bound each call.

When both `Text` and `HTML` are set the message is `multipart/alternative` with a
`text/plain` part followed by a `text/html` part, so a mail client shows the
first format it understands. With a single body it is a single part of that
type. `From` is optional and falls back to the configured sender. `Bcc`
recipients receive the message but never appear in its headers.

Every value that becomes an SMTP header — the subject, the sender header and
each recipient — is rejected if it contains a carriage return or a line feed, so
a crafted value cannot inject a header. Envelope addresses are parsed and
reduced to their bare form.

## Explicit TLS policy

`mail.tls` is a closed set and there is no opportunistic upgrade:

| value | behavior |
| --- | --- |
| `none` | an unencrypted connection. Credentials are refused with this policy, so they can never cross the wire in the clear |
| `starttls` | a plain connection, then a required `STARTTLS` upgrade before any credential is sent. A server that does not advertise `STARTTLS` fails the send |
| `implicit` | TLS from the first byte (SMTPS), before the SMTP greeting |

Production TLS requires TLS 1.2 or newer and verifies the server certificate.

## Configuration

The configuration lives in `internal/config` (`config.Mail`) and is read from
`config.yml` (`mail:` section) and overridden by the environment (`MAIL_HOST`,
`MAIL_PORT`, `MAIL_USERNAME`, `MAIL_PASSWORD`, `MAIL_FROM`, `MAIL_TLS`,
`MAIL_TIMEOUT_SECONDS`). The username and password are `config.Secret`, which
redacts itself in `fmt`, `log/slog`, JSON and YAML, so a credential cannot reach
a log record by accident. The port defaults to `587` and the timeout to `10`
seconds; the credentials never have a default. The `mail` section is validated
only when `New` builds a sender, so a project that installs mail but never sends
needs no mail setting.

`mailsender.New` takes a `mailsender.Provider`, the one-method interface
`MailSettings() config.Mail`. `*config.Config` satisfies it today, so the sender
reads YAML and the environment without knowing either. A future database-backed
site configuration implements that one method and replaces the YAML source
without changing the sender, this package or its tests.

## With Loom

If the project also has the Loom capability, `weld add mail` writes
`internal/di/mail_provider.go`, a stable seam that declares `NewMailSender`. The
mail capability deliberately edits no shared Loom file, so the generated graph
does **not** declare this provider the way it declares `NewRedisClient`. To
switch mail on:

1. Install `loom` before `mail`. `mail_provider.go` is written from the mail
   capability only, so it exists only when loom was already installed.
2. Add the binding to the graph by hand, in `internal/di/di.go`:

   ```go
   loom.Provide(NewMailSender),
   ```

   `di.go` is regenerated, so this line is erased by the next `weld add`; see
   step 3 for the durable alternative.
3. Make a provider the graph already consumes depend on `*mailsender.Sender`
   and edit the stable file that holds it (for an `api` + `loom` project, edit
   `internal/di/api_provider.go`). Loom then constructs `NewMailSender`,
   validates the mail configuration and injects the sender. That edit survives
   every later `weld add`.

Because `NewMailSender` never dials and never sends, constructing the graph
still sends no mail; the first `Send` is the first connection. Adding the
`{{- if .Caps.Has "mail"}}` block to the shared `loom` graph template would
remove step 2, but the mail capability intentionally leaves shared Loom files
untouched.

## Tests

The tests run a real in-process SMTP server on loopback: it speaks the SMTP
greeting, `EHLO`/`STARTTLS`/`AUTH`/`MAIL`/`RCPT`/`DATA`/`QUIT` protocol with a
self-signed certificate, and the sender talks to it through the real `net/smtp`
client, so the plain, STARTTLS and implicit-TLS paths are the real protocol
paths. No external SMTP server is needed:

```sh
go test ./internal/mailsender/...
```
