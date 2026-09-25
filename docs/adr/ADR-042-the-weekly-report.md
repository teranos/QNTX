# ADR-042: The weekly report

Date: 2026-09-25
Status: Proposed

"let's say QNTX also has it's own built in messages it would like to send sometimes via email"

"for a user that is one of the root identities, i want to receive a weekly report."

"y attestations got created per namespace"

"x users registered per namespace"

"z node restarts"

"downtime over last week"

"downtime over last 3 weeks"

"CPU over 7 days graph"

"MEM over 7 days graph"

"boot.subsystem.took over 7 days"

"7d total host.net.in"

"7d total host.net.out"

"query took over 7d, top three slowest query"

"host swap over 7d graph"

"top 3 4xx, top 3 5xx"

"top 3 handler failures"

"can you do it please to the degree that is feasible given sentry access?"

"I WANT TO SEE THE THREE MOST FREQUENTLY OCCURING ONES"

"AND FOR HANDLER THE SAME"

"I WANT TO SEE WHAT WAS TRIED TO ACCESS INSTEAD"

- The 4xx and 5xx are the three paths asked for most, with their status.
- The handler failures are the three errors handlers failed with most, with
  the handler.
- Neither shows how often.

## Where each part is read from

- The node's own records: attestations per namespace, Users registered per
  namespace (`auth.User` Namespace and CreatedAt), restarts (`node:started`),
  handler failures (failed Pulse jobs).
- Sentry: CPU, memory, swap, host.net.in and host.net.out, boot.subsystem.took,
  query took, the 4xx and 5xx request lines, and downtime.
- Downtime is the minutes in which the node sent Sentry no CPU sample: the
  node's own view of being down, sampled once a minute.
- The report goes to the ROOT User's primary address through MailService
  (ADR-041), and is attested like any other mail.

## Not done

- The three slowest queries. No query's text is recorded, so the report names
  the slowest times and not the queries.
- Downtime as seen from outside the box.
