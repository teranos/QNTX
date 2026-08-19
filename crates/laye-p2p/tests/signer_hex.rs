//! Prints the ed25519 public key behind a libp2p peer ID.
//! `cargo test -p laye-p2p --test signer_hex -- --nocapture`

#[test]
#[allow(
    clippy::expect_used,
    reason = "a one-off that prints or tells you why not"
)]
fn peer_id_to_signer_hex() {
    let peer_id_str = std::env::var("PEER_ID")
        .unwrap_or_else(|_| "12D3KooWC6UBnnmhhv3BAfYKyW1bFBD4GtC5waiEgQWJCb7Hbqaf".to_string());

    let peer_id: libp2p_identity::PeerId = peer_id_str.parse().expect("parse peer id");
    let digest = peer_id.as_ref().digest();
    let public = libp2p_identity::PublicKey::try_decode_protobuf(digest).expect("decode protobuf");
    let ed = public.try_into_ed25519().expect("ed25519");

    let mut hex = String::new();
    for b in ed.to_bytes() {
        hex.push_str(&format!("{b:02x}"));
    }
    println!("peer  {peer_id_str}");
    println!("hex   {hex}");
}
