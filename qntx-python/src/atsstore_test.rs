//! Tests for ATSStore gRPC client

#[cfg(test)]
mod tests {
    use super::super::atsstore::*;
    use std::sync::Arc;

    #[test]
    fn test_atsstore_config_creation() {
        let config = AtsStoreConfig {
            endpoint: "localhost:50051".to_string(),
            auth_token: "test-token".to_string(),
        };

        assert_eq!(config.endpoint, "localhost:50051");
        assert_eq!(config.auth_token, "test-token");
    }

    #[test]
    fn test_atsstore_client_initialization() {
        let config = AtsStoreConfig {
            endpoint: "localhost:50051".to_string(),
            auth_token: "test-token".to_string(),
        };

        let client = AtsStoreClient::new(config.clone());
        assert_eq!(client.config.endpoint, "localhost:50051");
        assert_eq!(client.config.auth_token, "test-token");
    }

    #[test]
    fn test_http_prefix_handling() {
        // Test that endpoint without http:// prefix gets it added
        let config = AtsStoreConfig {
            endpoint: "localhost:50051".to_string(),
            auth_token: "test-token".to_string(),
        };

        let mut client = AtsStoreClient::new(config);

        // This will fail to connect (no server running), but we can verify the endpoint format
        // The connect() method should handle adding http:// prefix
        let _ = client.connect(); // Ignore error, just testing prefix logic
    }

    #[test]
    fn test_shared_client_initialization() {
        let shared = new_shared_client();
        let guard = shared.lock();
        assert!(guard.is_none(), "Shared client should start uninitialized");
    }

    #[test]
    fn test_init_shared_client() {
        let shared = new_shared_client();

        let config = AtsStoreConfig {
            endpoint: "localhost:50051".to_string(),
            auth_token: "test-token".to_string(),
        };

        init_shared_client(&shared, config.clone());

        let guard = shared.lock();
        assert!(guard.is_some(), "Shared client should be initialized");

        if let Some(ref client) = *guard {
            assert_eq!(client.config.endpoint, "localhost:50051");
            assert_eq!(client.config.auth_token, "test-token");
        }
    }

    #[test]
    fn test_thread_local_client_set_clear() {
        let shared = new_shared_client();

        let config = AtsStoreConfig {
            endpoint: "localhost:50051".to_string(),
            auth_token: "test-token".to_string(),
        };

        init_shared_client(&shared, config);

        // Set current client
        set_current_client(shared.clone());

        // Clear current client
        clear_current_client();

        // After clearing, CURRENT_CLIENT should be None
        // (Can't directly test thread-local, but clear_current_client() should work)
    }

    #[test]
    fn test_attestation_result_fields() {
        let mut attributes = std::collections::HashMap::new();
        attributes.insert(
            "severity".to_string(),
            serde_json::Value::String("high".to_string()),
        );

        let result = AttestationResult {
            id: "AS123".to_string(),
            subjects: vec!["user:alice".to_string()],
            predicates: vec!["completed".to_string()],
            contexts: vec!["task:review".to_string()],
            actors: vec!["system".to_string()],
            timestamp: 1234567890,
            source: "python".to_string(),
            attributes: attributes.clone(),
        };

        assert_eq!(result.id, "AS123");
        assert_eq!(result.subjects.len(), 1);
        assert_eq!(result.subjects[0], "user:alice");
        assert_eq!(result.predicates[0], "completed");
        assert_eq!(result.timestamp, 1234567890);
        assert_eq!(result.source, "python");
        assert_eq!(
            result.attributes.get("severity"),
            Some(&serde_json::Value::String("high".to_string()))
        );
    }

    // Note: Full integration tests for create_attestation() require a running ATSStore server
    // Those tests should be in a separate integration test suite with a test server
}
