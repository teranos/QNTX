# ADR-046: A plugin stands in a namespace

Date: 2026-09-30
Status: Proposed

Asked on 2026-09-30 whether cleanAPI belongs in the Clean namespace, and
answered yes, by ROOT, in that business's own week file.

## The problem

Every plugin's services are started on the served universe, which is default
(server/sub_plugins.go). The shared token a plugin is handed at Initialize
reaches that store and no other. cleanAPI wrote every me-save, me-claim,
coverage-set and rates row into default, while the site's Users, the stand's
arrivals and the ROOT session at the site's door stand in Clean. Its
/am/areas read a store the stand never writes into (WriteAsPublic refuses
default) and answered nothing by construction. A per-call token reaches the
caller's namespace on the sigil path only; a plugin's own schedules and its
plain HTTP routes had no way into any namespace but default.

## Decided

A plugin's record may name the namespace it stands in, under the key
`namespace`, beside the `build.*` keys. It is QNTX's key, not the plugin's:
the plugin's ConfigSchema does not name it and does not validate it. The
plugin element is where it is written, as every record key is (ADR-043).

At Initialize the node mints such a plugin a token of its own, reaching the
store of that namespace at the ATS store service and the fetch service. The
shared token stays what it is for every plugin whose record names none. A new
token is minted at each Initialize, so a record that moved a plugin moves
where it reads and writes, and the token before it reaches nothing.

The door is `Held.OfPlugin`. It refuses system: no plugin acts where the node
keeps its own records. What stands behind the door is the record, which ROOT
wrote.

A namespace the node cannot serve fails the plugin's Initialize with the
reason, never a plugin started on an empty token.

## Not decided here

What a plugin wrote into default before it was moved stays in default. Nothing
migrates it.
