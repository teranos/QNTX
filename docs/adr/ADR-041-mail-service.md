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

## Transport

"ses being enabled for use with email service can be enabled in the am.toml"

## Not done

- Bounces, complaints and replies coming back.
