# ADR-047: Real inboxes

Date: 2026-10-01
Status: Proposed

"mail, per user, in the system"

"everyone get's their own mail address, and they get it via ROOT approval"

"a normal mail client, boring, fast, reliable, send and receive mail, spambox, etc."

"I want SES in both directions"

"no search, no drafts"

"MailService Provider Plugin"

"both meaning, the box sending mail (because its dying) vs A User with an inbox who has mail and wants to get back to it later"

"it will also be namespace specific, and cant be system of default"

"A user can have multiple inboxes"

"Add SUPER to it"

"ROOT should be able to read other user's mail as well, but doing so is an attested event."

"[At least 7 characters]@domain.tld"

"I also expect to be able to send mail as another user as ROOT, and this should also be an attested event"

"Let's say, via mail, now i would like to be able to send pdf's if i wanted to."

## Standard

JMAP for Mail, RFC 8621. The message is RFC 5322; mailbox roles are RFC 6154.

## Divergence

RFC 8621's `Identity` is `MailIdentity`. In QNTX identity is taken: ADR-010 is
the identity system, ADR-030 covers identity providers, and ADR-031 attests
`identity:disabled` and `identity:enabled`.

An `Email` is an attestation, not a JMAP object: `mail:received` and
`mail:sent` of the address, the text in its attributes, an attachment by name, size and sha256 beside the stored mail. Its id is the ASID.

A mailbox is one of three roles, `inbox`, `junk` and `sent`, and has no
object of its own. SES's spam verdict is what puts mail in `junk`.

A message SES's virus scan fails is never an `Email`: it is `mail:dropped`,
and the stored copy is deleted.
