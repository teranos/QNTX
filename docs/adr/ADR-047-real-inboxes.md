# ADR-047: Real inboxes

Date: 2026-10-01
Status: Proposed

"mail, per user, in the system"

"everyone get's their own mail address, and they get it via ROOT approval"

"a normal mail client, boring, fast, reliable, send and receive mail, spambox, etc."

"I want SES in both directions"

"MailService Provider Plugin"

"both meaning, the box sending mail (because its dying) vs A User with an inbox who has mail and wants to get back to it later"

## Standard

JMAP for Mail, RFC 8621. The message is RFC 5322; mailbox roles are RFC 6154.

## Divergence

RFC 8621's `Identity` is `MailIdentity`. In QNTX identity is taken: ADR-010 is
the identity system, ADR-030 covers identity providers, and ADR-031 attests
`identity:disabled` and `identity:enabled`.
