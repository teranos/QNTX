# Signa

Every signum the node holds or will hold, a table each, a row per sigil (ADR-039). The mark says whether the sigil is real: `done` is served from its sigil over every surface, empty is not yet, `wont` is decided against. The names are the sigils' own; where a sigil is not yet real its name is provisional.

## staands

| sigil | mark |
| --- | --- |
| list | done |
| create | done |
| take-down | done |
| metrics | done |
| activity | done |
| visits | done |

## watchers

| sigil | mark |
| --- | --- |
| list | |
| create | |
| read | |
| update | |
| delete | |
| queue stats | |

## namespaces

| sigil | mark |
| --- | --- |
| list | |
| create | |
| disable | |
| enable | |
| delete | |
| nuke default | |

## types

| sigil | mark |
| --- | --- |
| list | |
| create or update | |
| read | |

## plugins

| sigil | mark |
| --- | --- |
| list | |
| glyphs | |
| routes | |
| read config | |
| update config | |
| logs | |
| pause | |
| resume | |
| restart | |
| enable | |
| disable | |

## canvas

By subject this is one signum; by handler it is four (glyphs, compositions, minimized windows, export). Undecided.

| sigil | mark |
| --- | --- |
| glyphs list | |
| glyphs create | |
| glyphs read | |
| glyphs delete | |
| compositions list | |
| compositions create | |
| compositions read | |
| compositions delete | |
| minimized windows list | |
| minimized windows add | |
| minimized windows remove | |
| export | |
| export-dom | |

## pulse

| sigil | mark |
| --- | --- |
| schedules list | |
| schedules create | |
| schedules read | |
| schedules update | |
| schedules delete | |
| jobs list | |
| jobs read | |
| jobs executions | |
| jobs children | |
| jobs stages | |
| jobs task logs | |
| execution read | |
| execution logs | |

## attestations

The answer to query is a bare array today, and the cut has nowhere to go but a header. Its sigil gives an object.

| sigil | mark |
| --- | --- |
| query | |
| create | |

## roles

| sigil | mark |
| --- | --- |
| list | |

## tokens

Under `/auth` with the ceremony, which is routes; split by hand.

| sigil | mark |
| --- | --- |
| list | |
| mint | |
| read | |
| revoke | |
| lift the revocation | |

## users

Under `/auth` with the ceremony, which is routes; split by hand.

| sigil | mark |
| --- | --- |
| list | |
| disable | |
| enable | |

## i

| sigil | mark |
| --- | --- |
| who I am | |
| standing | |
| disable | |
| enable | |

## am

| sigil | mark |
| --- | --- |
| version | |
| syscap | |
| statusline | |
| statusline item | |

## embeddings

One handler per path. `search` is answered by the embeddings handler and may belong here.

| sigil | mark |
| --- | --- |
| generate | |
| batch | |
| cluster | |
| clusters | |
| samples | |
| members | |
| memberships | |
| by-source | |
| timeline | |
| info | |
| unembedded | |
| project | |
| projections | |

## search

| sigil | mark |
| --- | --- |
| semantic | |

## files

Serve answers bytes, not JSON, so it gives no fields.

| sigil | mark |
| --- | --- |
| upload | |
| serve | |

## python

| sigil | mark |
| --- | --- |
| execute | |

## prose

server/README.md says prose may be deprecated.

| sigil | mark |
| --- | --- |
| tree | |
| read | |
| update | |

## prompt

Seen as a prefix path only.

| sigil | mark |
| --- | --- |
| prompt | |

## glyph-config

| sigil | mark |
| --- | --- |
| read | |
| write | |

## timeseries

| sigil | mark |
| --- | --- |
| usage | |

## dev

`dev` answers plain text, so it gives no fields.

| sigil | mark |
| --- | --- |
| dev | |
| debug add | |
| debug read | |
| crash-test | |

## logs

| sigil | mark |
| --- | --- |
| download | |

## openapi

Once the document is written from the signa this may stop being a sigil.

| sigil | mark |
| --- | --- |
| document | |

## Routes, not sigils

Decided in ADR-039. No signum holds them and no tool is made for them.

| route | mark |
| --- | --- |
| .well-known | wont |
| the /auth ceremony | wont |
| / and /health | wont |
| /g/ | wont |
| /s/ | wont |
| the sockets | wont |
| /mcp | wont |
