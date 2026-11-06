# ADR-035 — Datapunts

**Status:** Accepted and Implemented. 
**Date:** 2026-09-07 
**Related:** [[ADR-025-access-tokens]] (Access Tokens), [[ADR-026-Namespaces]] (Namespaces), [[ADR-030-identity-providers]] (Identity Providers), ADR-031 (The User), [[ADR-032-Doors]] (Front Doors), [[ADR-034-roles-as-attestations]] (Roles as Attestations)

Analytics Pixel

answers on `/d/{market}/{slug}`

"Staand needs to be renamed to Datapunt everywhere"

- "So Datapunt is the Pixel"
- "One web analytics pixel is one datapunt"
- "A visit is for datapunt to observe, as it's doing now"
- "CUE allows even web Analytics Pixels to be dynamically defined"
- "CUE schema changes need approvals from User in a way that's sensible, goes for both Datapunt and the converted Staands"

"make it /d/ for datapixel"

The node answers the hit and writes the arrival, sourced `datapixel`; the
datapunt plugin declares in CUE what a pixel's snippet may say, and a schema
change is approved by merging it. Staand records, `staand:*`, are not read.


---


Wants:

- Real image as pixel
- DNS datapunt
- PDF datapunt
- QR datapunt
- CF+S3 as datapunt
- Redirect Datapunt