#[cfg(test)]
mod tests {
    use crate::Attestation;

    #[test]
    fn test_attestation_type_exists() {
        let attestation = Attestation {
            id: "test-id".to_string(),
            subjects: vec!["subject1".to_string()],
            predicates: vec!["predicate1".to_string()],
            contexts: vec![],
            actors: vec![],
            timestamp: 1234567890,
            source: "test".to_string(),
            attributes: None,
            created_at: 1234567890,
        };

        // Test JSON serialization works
        let json = serde_json::to_string(&attestation).unwrap();
        assert!(json.contains("test-id"));

        // Test deserialization
        let _parsed: Attestation = serde_json::from_str(&json).unwrap();
    }
}
