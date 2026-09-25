# ADR-041: MailService

Date: 2026-09-24
Status: Proposed

- MailService is a service QNTX core provides, not a plugin. A plugin calls it
  over gRPC, and the plugin provides the template.
- It is served the way FetchService is: core does the outbound act for the
  plugin, and its endpoint reaches the plugin in `InitializeRequest`.
- Core sends through Amazon SES.

## Recipient

"a user"

## Record

"its attested"

## Governance

"this is an auth question, deferred, assume only ROOT has governance over gRPC calls"

## Address

"goes to their primary email address if there are multiple."

The primary is the first address a User supplied.

## Template

"the plugin owns the template but qntx does provide a neutral template and code for how to set it"

"that is a ROOT question about governance and controls and monitoring that belongs in its own window element in the tray like others"

The window holds the templates, the mail sent, and the SES account.

## Images

"plugins can send images"

"Add a size cap and accept image/png only."

A Send carries images inline; the html shows each by `cid:<content_id>`.

## Transport

"ses being enabled for use with email service can be enabled in the am.toml"

## Dark

"but i do still want the dark themed qntx tokens css email template"

QNTX keeps a second template, dark.

"why not just use the real source"

"it just needs to fit with the rest of qntx"

A mail QNTX draws is an element window on its canvas, as ≡ am is: a title
bar, then sections of labelled rows. Its values are read from web/css, which
the binary embeds. Tables and inline styles carry it, because every mail
client lays those out the same way. The weekly report is drawn the same way.

## Not done

- Bounces, complaints and replies coming back.
