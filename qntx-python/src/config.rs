//! Plugin configuration types

use std::collections::HashMap;

/// Plugin configuration received during initialization
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

/// Build the configuration schema for the Python plugin
///
/// This defines all configuration fields that the plugin accepts,
/// including their types, descriptions, and validation constraints.
pub fn build_schema() -> HashMap<String, crate::proto::ConfigFieldSchema> {
    use crate::proto::ConfigFieldSchema;

    let mut fields = HashMap::new();

    fields.insert(
        "python_paths".to_string(),
        ConfigFieldSchema {
            r#type: "string".to_string(),
            description: "Colon-separated list of Python module search paths".to_string(),
            default_value: String::new(),
            required: false,
            min_value: String::new(),
            max_value: String::new(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields.insert(
        "default_modules".to_string(),
        ConfigFieldSchema {
            r#type: "string".to_string(),
            description: "Comma-separated list of Python modules to auto-import".to_string(),
            default_value: "numpy,pandas,scipy,matplotlib".to_string(),
            required: false,
            min_value: String::new(),
            max_value: String::new(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields.insert(
        "timeout_secs".to_string(),
        ConfigFieldSchema {
            r#type: "number".to_string(),
            description: "Maximum execution time for Python code in seconds".to_string(),
            default_value: "30".to_string(),
            required: false,
            min_value: "1".to_string(),
            max_value: "300".to_string(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields.insert(
        "max_workers".to_string(),
        ConfigFieldSchema {
            r#type: "number".to_string(),
            description: "Maximum number of concurrent Python execution workers".to_string(),
            default_value: "4".to_string(),
            required: false,
            min_value: "1".to_string(),
            max_value: "16".to_string(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields.insert(
        "enable_debug".to_string(),
        ConfigFieldSchema {
            r#type: "boolean".to_string(),
            description: "Enable debug logging for Python execution".to_string(),
            default_value: "false".to_string(),
            required: false,
            min_value: String::new(),
            max_value: String::new(),
            pattern: String::new(),
            element_type: String::new(),
        },
    );

    fields
}
