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

## Dark

"but i do still want the dark themed qntx tokens css email template"

QNTX keeps a second template, dark, drawn from web/css/tokens.css.

"what is the bg color of the canvas, what pattern does it use, what are the colors of the ax element, and the type element, and the sigma element, and the attestation element, and the triplet."

A mail QNTX draws stands on its canvas, grid and all, as windows, each in
the ink of one of QNTX's elements. The weekly report is drawn the same way.

## Not done

- Bounces, complaints and replies coming back.
