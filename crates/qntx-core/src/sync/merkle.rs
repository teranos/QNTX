//! Merkle tree state digest for attestation sync.
//!
//! Groups attestations by (actor, context) pairs — mirroring bounded storage —
//! and computes hierarchical hashes for efficient set reconciliation.

use serde::{Deserialize, Serialize};
use sha2::{Digest, Sha256};
use std::collections::{BTreeMap, BTreeSet};

use super::content::{hex_decode, hex_encode};

/// Identifies a bounded storage group: one (actor, context) pair.
#[derive(Debug, Clone, PartialEq, Eq, PartialOrd, Ord, Hash, Serialize, Deserialize)]
pub struct GroupKey {
    pub actor: String,
    pub context: String,
}

/// In-memory Merkle tree mirroring the bounded storage hierarchy.
///
/// Uses BTreeMap/BTreeSet for deterministic iteration order, eliminating
/// the need for explicit sorting during hash computation.
pub struct MerkleTree {
    groups: BTreeMap<[u8; 32], Group>,
    dirty: bool,
    root: [u8; 32],
}

struct Group {
    key: GroupKey,
    leaves: BTreeSet<[u8; 32]>,
    dirty: bool,
    hash: [u8; 32],
}

impl MerkleTree {
    pub fn new() -> Self {
        Self {
            groups: BTreeMap::new(),
            dirty: false,
            root: [0u8; 32],
        }
    }

    /// Insert an attestation content hash under the given group.
    pub fn insert(&mut self, key: GroupKey, content_hash: [u8; 32]) {
        let gkh = group_key_hash(&key);
        let group = self.groups.entry(gkh).or_insert_with(|| Group {
            key,
            leaves: BTreeSet::new(),
            dirty: true,
            hash: [0u8; 32],
        });

        if group.leaves.insert(content_hash) {
            group.dirty = true;
            self.dirty = true;
        }
    }

    /// Remove an attestation content hash from the given group.
    pub fn remove(&mut self, key: &GroupKey, content_hash: &[u8; 32]) {
        let gkh = group_key_hash(key);
        if let Some(group) = self.groups.get_mut(&gkh) {
            if group.leaves.remove(content_hash) {
                group.dirty = true;
                self.dirty = true;
            }
            if group.leaves.is_empty() {
                self.groups.remove(&gkh);
            }
        }
    }

    /// Returns true if the content hash exists in any group.
    pub fn contains(&self, content_hash: &[u8; 32]) -> bool {
        self.groups.values().any(|g| g.leaves.contains(content_hash))
    }

    /// Get the root hash. Recomputes lazily when dirty.
    pub fn root(&mut self) -> [u8; 32] {
        if self.dirty {
            self.recompute();
        }
        self.root
    }

    /// Get all group key hash → group hash pairs.
    pub fn group_hashes(&mut self) -> BTreeMap<[u8; 32], [u8; 32]> {
        let mut result = BTreeMap::new();
        for (gkh, group) in &mut self.groups {
            if group.dirty {
                group.recompute_hash();
            }
            result.insert(*gkh, group.hash);
        }
        result
    }

    /// Compute diff against remote group hashes.
    /// Returns (local_only, remote_only, divergent) group key hashes.
    pub fn diff(
        &mut self,
        remote: &BTreeMap<[u8; 32], [u8; 32]>,
    ) -> (Vec<[u8; 32]>, Vec<[u8; 32]>, Vec<[u8; 32]>) {
        let local = self.group_hashes();

        let mut local_only = Vec::new();
        let mut divergent = Vec::new();

        for (gkh, lhash) in &local {
            match remote.get(gkh) {
                None => local_only.push(*gkh),
                Some(rhash) if rhash != lhash => divergent.push(*gkh),
                _ => {}
            }
        }

        let remote_only: Vec<[u8; 32]> = remote
            .keys()
            .filter(|gkh| !local.contains_key(*gkh))
            .copied()
            .collect();

        (local_only, remote_only, divergent)
    }

    /// Find the GroupKey for a group key hash.
    pub fn find_group_key(&self, gkh: &[u8; 32]) -> Option<&GroupKey> {
        self.groups.get(gkh).map(|g| &g.key)
    }

    /// Total attestation content hashes across all groups.
    pub fn size(&self) -> usize {
        self.groups.values().map(|g| g.leaves.len()).sum()
    }

    /// Number of (actor, context) groups.
    pub fn group_count(&self) -> usize {
        self.groups.len()
    }

    fn recompute(&mut self) {
        if self.groups.is_empty() {
            self.root = [0u8; 32];
            self.dirty = false;
            return;
        }

        let mut h = Sha256::new();
        h.update(b"root:");
        // BTreeMap iterates in sorted order — deterministic without explicit sort
        for group in self.groups.values_mut() {
            if group.dirty {
                group.recompute_hash();
            }
            h.update(group.hash);
        }
        self.root = h.finalize().into();
        self.dirty = false;
    }
}

impl Default for MerkleTree {
    fn default() -> Self {
        Self::new()
    }
}

impl Group {
    fn recompute_hash(&mut self) {
        if self.leaves.is_empty() {
            self.hash = [0u8; 32];
            self.dirty = false;
            return;
        }

        let mut h = Sha256::new();
        h.update(b"grp:");
        h.update(self.key.actor.as_bytes());
        h.update(b"\0");
        h.update(self.key.context.as_bytes());
        h.update(b"\0");
        // BTreeSet iterates in sorted order — deterministic
        for leaf in &self.leaves {
            h.update(leaf);
        }
        self.hash = h.finalize().into();
        self.dirty = false;
    }
}

/// Compute deterministic hash of a GroupKey.
fn group_key_hash(key: &GroupKey) -> [u8; 32] {
    let mut h = Sha256::new();
    h.update(b"gk:");
    h.update(key.actor.as_bytes());
    h.update(b"\0");
    h.update(key.context.as_bytes());
    h.finalize().into()
}

// ============================================================================
// JSON entry points for WASM bridge
// ============================================================================
// The Merkle tree is stateful, so WASM targets use thread-local storage.
// These functions operate on a global tree instance (set up by the bridge).

use std::cell::RefCell;

thread_local! {
    static TREE: RefCell<MerkleTree> = RefCell::new(MerkleTree::new());
}

/// Insert into the global Merkle tree.
/// Input: `{"actor":"...","context":"...","content_hash":"<hex>"}`
/// Output: `{"ok":true}` or `{"error":"..."}`
pub fn merkle_insert_json(input: &str) -> String {
    #[derive(Deserialize)]
    struct Input {
        actor: String,
        context: String,
        content_hash: String,
    }

    let parsed: Input = match serde_json::from_str(input) {
        Ok(v) => v,
        Err(e) => return format!(r#"{{"error":"invalid JSON: {}"}}"#, e),
    };

    let hash = match hex_decode(&parsed.content_hash) {
        Some(h) => h,
        None => return r#"{"error":"invalid hex content_hash (expected 64 chars)"}"#.into(),
    };

    TREE.with(|t| {
        t.borrow_mut().insert(
            GroupKey {
                actor: parsed.actor,
                context: parsed.context,
            },
            hash,
        )
    });

    r#"{"ok":true}"#.into()
}

/// Remove from the global Merkle tree.
/// Input: `{"actor":"...","context":"...","content_hash":"<hex>"}`
/// Output: `{"ok":true}`
pub fn merkle_remove_json(input: &str) -> String {
    #[derive(Deserialize)]
    struct Input {
        actor: String,
        context: String,
        content_hash: String,
    }

    let parsed: Input = match serde_json::from_str(input) {
        Ok(v) => v,
        Err(e) => return format!(r#"{{"error":"invalid JSON: {}"}}"#, e),
    };

    let hash = match hex_decode(&parsed.content_hash) {
        Some(h) => h,
        None => return r#"{"error":"invalid hex content_hash (expected 64 chars)"}"#.into(),
    };

    TREE.with(|t| {
        t.borrow_mut().remove(
            &GroupKey {
                actor: parsed.actor,
                context: parsed.context,
            },
            &hash,
        )
    });

    r#"{"ok":true}"#.into()
}

/// Check if a content hash exists in the global Merkle tree.
/// Input: `{"content_hash":"<hex>"}`
/// Output: `{"exists":true}` or `{"exists":false}`
pub fn merkle_contains_json(input: &str) -> String {
    #[derive(Deserialize)]
    struct Input {
        content_hash: String,
    }

    let parsed: Input = match serde_json::from_str(input) {
        Ok(v) => v,
        Err(e) => return format!(r#"{{"error":"invalid JSON: {}"}}"#, e),
    };

    let hash = match hex_decode(&parsed.content_hash) {
        Some(h) => h,
        None => return r#"{"error":"invalid hex content_hash (expected 64 chars)"}"#.into(),
    };

    let exists = TREE.with(|t| t.borrow().contains(&hash));
    format!(r#"{{"exists":{}}}"#, exists)
}

/// Get the global tree's root hash.
/// Output: `{"root":"<hex>","size":N,"groups":N}`
pub fn merkle_root_json(_input: &str) -> String {
    TREE.with(|t| {
        let mut tree = t.borrow_mut();
        let root = tree.root();
        let size = tree.size();
        let groups = tree.group_count();
        format!(
            r#"{{"root":"{}","size":{},"groups":{}}}"#,
            hex_encode(root),
            size,
            groups
        )
    })
}

/// Get all group hashes from the global tree.
/// Output: `{"groups":{"<hex_gkh>":"<hex_ghash>",...}}`
pub fn merkle_group_hashes_json(_input: &str) -> String {
    TREE.with(|t| {
        let mut tree = t.borrow_mut();
        let hashes = tree.group_hashes();
        let map: BTreeMap<String, String> = hashes
            .into_iter()
            .map(|(k, v)| (hex_encode(k), hex_encode(v)))
            .collect();
        match serde_json::to_string(&map) {
            Ok(json) => format!(r#"{{"groups":{}}}"#, json),
            Err(e) => format!(r#"{{"error":"serialization failed: {}"}}"#, e),
        }
    })
}

/// Diff global tree against remote group hashes.
/// Input: `{"remote":{"<hex_gkh>":"<hex_ghash>",...}}`
/// Output: `{"local_only":["<hex>",...],"remote_only":[...],"divergent":[...]}`
pub fn merkle_diff_json(input: &str) -> String {
    #[derive(Deserialize)]
    struct Input {
        remote: BTreeMap<String, String>,
    }

    let parsed: Input = match serde_json::from_str(input) {
        Ok(v) => v,
        Err(e) => return format!(r#"{{"error":"invalid JSON: {}"}}"#, e),
    };

    let mut remote = BTreeMap::new();
    for (k_hex, v_hex) in &parsed.remote {
        let k = match hex_decode(k_hex) {
            Some(h) => h,
            None => return format!(r#"{{"error":"invalid hex group key: {}"}}"#, k_hex),
        };
        let v = match hex_decode(v_hex) {
            Some(h) => h,
            None => return format!(r#"{{"error":"invalid hex group hash: {}"}}"#, v_hex),
        };
        remote.insert(k, v);
    }

    TREE.with(|t| {
        let mut tree = t.borrow_mut();
        let (local_only, remote_only, divergent) = tree.diff(&remote);

        let lo: Vec<String> = local_only.iter().map(|h| hex_encode(*h)).collect();
        let ro: Vec<String> = remote_only.iter().map(|h| hex_encode(*h)).collect();
        let dv: Vec<String> = divergent.iter().map(|h| hex_encode(*h)).collect();

        match (
            serde_json::to_string(&lo),
            serde_json::to_string(&ro),
            serde_json::to_string(&dv),
        ) {
            (Ok(lo_json), Ok(ro_json), Ok(dv_json)) => {
                format!(
                    r#"{{"local_only":{},"remote_only":{},"divergent":{}}}"#,
                    lo_json, ro_json, dv_json
                )
            }
            _ => r#"{"error":"serialization failed"}"#.into(),
        }
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    fn hash_bytes(s: &str) -> [u8; 32] {
        let mut h = Sha256::new();
        h.update(s.as_bytes());
        h.finalize().into()
    }

    #[test]
    fn empty_tree() {
        let mut tree = MerkleTree::new();
        assert_eq!(tree.root(), [0u8; 32]);
        assert_eq!(tree.size(), 0);
        assert_eq!(tree.group_count(), 0);
    }

    #[test]
    fn insert_changes_root() {
        let mut tree = MerkleTree::new();
        let empty = tree.root();
        tree.insert(
            GroupKey {
                actor: "a".into(),
                context: "c".into(),
            },
            hash_bytes("att-1"),
        );
        assert_ne!(tree.root(), empty);
    }

    #[test]
    fn deterministic_root() {
        let mut a = MerkleTree::new();
        let mut b = MerkleTree::new();

        let k1 = GroupKey {
            actor: "a1".into(),
            context: "c1".into(),
        };
        let k2 = GroupKey {
            actor: "a2".into(),
            context: "c2".into(),
        };
        let h1 = hash_bytes("1");
        let h2 = hash_bytes("2");
        let h3 = hash_bytes("3");

        a.insert(k1.clone(), h1);
        a.insert(k1.clone(), h2);
        a.insert(k2.clone(), h3);

        // Insert in different order
        b.insert(k2, h3);
        b.insert(k1.clone(), h2);
        b.insert(k1, h1);

        assert_eq!(a.root(), b.root());
    }

    #[test]
    fn remove_restores_empty() {
        let mut tree = MerkleTree::new();
        let key = GroupKey {
            actor: "a".into(),
            context: "c".into(),
        };
        let h = hash_bytes("att");

        tree.insert(key.clone(), h);
        tree.remove(&key, &h);
        assert_eq!(tree.root(), [0u8; 32]);
        assert_eq!(tree.size(), 0);
    }

    #[test]
    fn contains() {
        let mut tree = MerkleTree::new();
        let key = GroupKey {
            actor: "a".into(),
            context: "c".into(),
        };
        let h = hash_bytes("att");

        assert!(!tree.contains(&h));
        tree.insert(key, h);
        assert!(tree.contains(&h));
    }

    #[test]
    fn diff_identical() {
        let mut a = MerkleTree::new();
        let mut b = MerkleTree::new();

        let key = GroupKey {
            actor: "a".into(),
            context: "c".into(),
        };
        let h = hash_bytes("att");

        a.insert(key.clone(), h);
        b.insert(key, h);

        let remote = b.group_hashes();
        let (lo, ro, dv) = a.diff(&remote);
        assert!(lo.is_empty());
        assert!(ro.is_empty());
        assert!(dv.is_empty());
    }

    #[test]
    fn diff_divergent() {
        let mut a = MerkleTree::new();
        let mut b = MerkleTree::new();

        let key = GroupKey {
            actor: "a".into(),
            context: "c".into(),
        };

        a.insert(key.clone(), hash_bytes("att-1"));
        b.insert(key, hash_bytes("att-2"));

        let remote = b.group_hashes();
        let (lo, ro, dv) = a.diff(&remote);
        assert!(lo.is_empty());
        assert!(ro.is_empty());
        assert_eq!(dv.len(), 1);
    }

    #[test]
    fn diff_local_only() {
        let mut a = MerkleTree::new();
        let b = MerkleTree::default();

        a.insert(
            GroupKey {
                actor: "a".into(),
                context: "c".into(),
            },
            hash_bytes("att"),
        );

        let remote = BTreeMap::new();
        let (lo, ro, dv) = a.diff(&remote);
        assert_eq!(lo.len(), 1);
        assert!(ro.is_empty());
        assert!(dv.is_empty());
        let _ = b; // silence unused warning
    }

    #[test]
    fn different_groups_same_leaves() {
        let mut tree = MerkleTree::new();
        let k1 = GroupKey {
            actor: "a1".into(),
            context: "c".into(),
        };
        let k2 = GroupKey {
            actor: "a2".into(),
            context: "c".into(),
        };
        let h = hash_bytes("same");

        tree.insert(k1, h);
        tree.insert(k2, h);

        let hashes = tree.group_hashes();
        let values: Vec<_> = hashes.values().collect();
        assert_eq!(values.len(), 2);
        assert_ne!(values[0], values[1]);
    }

    #[test]
    fn json_insert_and_root() {
        // Reset global tree
        TREE.with(|t| *t.borrow_mut() = MerkleTree::new());

        let hash = hex_encode(hash_bytes("test"));
        let input = format!(
            r#"{{"actor":"a","context":"c","content_hash":"{}"}}"#,
            hash
        );
        let result = merkle_insert_json(&input);
        assert!(result.contains(r#""ok":true"#), "got: {}", result);

        let root = merkle_root_json("");
        let parsed: serde_json::Value = serde_json::from_str(&root).unwrap();
        assert_eq!(parsed["size"], 1);
        assert_eq!(parsed["groups"], 1);
        assert_ne!(parsed["root"].as_str().unwrap(), &hex_encode([0u8; 32]));
    }

    #[test]
    fn json_diff() {
        TREE.with(|t| *t.borrow_mut() = MerkleTree::new());

        let hash = hex_encode(hash_bytes("local-only"));
        merkle_insert_json(&format!(
            r#"{{"actor":"a","context":"c","content_hash":"{}"}}"#,
            hash
        ));

        // Diff against empty remote
        let result = merkle_diff_json(r#"{"remote":{}}"#);
        let parsed: serde_json::Value = serde_json::from_str(&result).unwrap();
        assert_eq!(parsed["local_only"].as_array().unwrap().len(), 1);
        assert!(parsed["remote_only"].as_array().unwrap().is_empty());
        assert!(parsed["divergent"].as_array().unwrap().is_empty());
    }

    #[test]
    fn json_contains() {
        TREE.with(|t| *t.borrow_mut() = MerkleTree::new());

        let hash = hex_encode(hash_bytes("findme"));
        merkle_insert_json(&format!(
            r#"{{"actor":"a","context":"c","content_hash":"{}"}}"#,
            hash
        ));

        let result = merkle_contains_json(&format!(r#"{{"content_hash":"{}"}}"#, hash));
        assert!(result.contains(r#""exists":true"#), "got: {}", result);

        let missing = hex_encode(hash_bytes("missing"));
        let result = merkle_contains_json(&format!(r#"{{"content_hash":"{}"}}"#, missing));
        assert!(result.contains(r#""exists":false"#), "got: {}", result);
    }
}
