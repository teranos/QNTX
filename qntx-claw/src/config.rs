//! Plugin configuration types

use std::collections::HashMap;

/// Plugin configuration received during initialization.
#[derive(Debug, Clone, Default)]
pub struct PluginConfig {
    /// ATSStore gRPC endpoint
    pub ats_store_endpoint: String,
    /// Queue service gRPC endpoint
    pub queue_endpoint: String,
    /// Auth token for service calls
    pub auth_token: String,
    /// Custom configuration values
    pub config: HashMap<String, String>,
}

/// Build the configuration schema for the claw plugin.
pub fn build_schema() -> HashMap<String, crate::proto::ConfigFieldSchema> {
    use crate::proto::ConfigFieldSchema;

    let mut fields = HashMap::new();

    fields.insert(
        "workspace_path".to_string(),
        ConfigFieldSchema {
            r#type: "string".to_string(),
            description: "Path to the OpenClaw workspace directory. Auto-discovered if empty."
                .to_string(),
            default_value: String::new(),
            required: false,
            min_value: String::new(),
            max_value: String::new(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields.insert(
        "watch_enabled".to_string(),
        ConfigFieldSchema {
            r#type: "boolean".to_string(),
            description: "Enable file watching for live updates on workspace changes.".to_string(),
            default_value: "true".to_string(),
            required: false,
            min_value: String::new(),
            max_value: String::new(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields
}
