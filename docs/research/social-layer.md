# Social Layer Research

## Current State

AT Protocol plugin implemented. Posts, follows, likes, timeline, notifications all work. Timeline sync creates attestations (`post-uri appeared-in-timeline atproto {author, text}`).

## Open Questions

**Should QNTX be a social client?** Post, follow, like directly from QNTX UI? Or just consume feeds for knowledge work?

**What attestations should notifications create?** Every mention? Replies? Follows? High volume.

**How do multiple protocols work together?** AT Protocol + ActivityPub. Unified timeline? Cross-protocol identity mapping?

**What's the UX for social actions in the graph?** Click a handle to see their posts? Follow button on attestation glyphs?

**Feed vs sync distinction?** `/timeline` returns JSON. Sync creates attestations. When do you want each?
