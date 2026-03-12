use crate::proto::{HttpHeader, HttpResponse};
use parking_lot::RwLock;
use pyo3::prelude::*;
use pyo3::types::PyList;
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use std::sync::Arc;
use std::time::Instant;
use tonic::Status;
use tracing::{error, info, warn};

/// Known reduction methods.
const KNOWN_METHODS: &[&str] = &["umap", "tsne", "pca"];

/// Per-method fit state.
#[derive(Clone)]
pub(crate) struct MethodState {
    pub n_points: usize,
    pub n_components: usize,
    pub supports_transform: bool,
}

/// Plugin state holding per-method fitted model references.
pub(crate) struct ReduceState {
    pub fitted: HashMap<String, MethodState>,
}

/// Handler context wrapping shared state.
pub struct HandlerContext {
    pub(crate) state: Arc<RwLock<ReduceState>>,
}

impl HandlerContext {
    pub(crate) fn new(state: Arc<RwLock<ReduceState>>) -> Self {
        Self { state }
    }

    /// POST /fit — fit a dimensionality reduction model and return projections.
    pub fn handle_fit(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
        #[derive(Deserialize)]
        struct FitRequest {
            embeddings: Vec<Vec<f32>>,
            #[serde(default = "default_method")]
            method: String,
            #[serde(default = "default_n_components")]
            n_components: usize,
            #[serde(default = "default_n_neighbors")]
            n_neighbors: usize,
            #[serde(default = "default_min_dist")]
            min_dist: f64,
            #[serde(default = "default_metric")]
            metric: String,
            #[serde(default = "default_perplexity")]
            perplexity: f64,
        }

        fn default_method() -> String {
            "umap".to_string()
        }
        fn default_n_components() -> usize {
            2
        }
        fn default_n_neighbors() -> usize {
            15
        }
        fn default_min_dist() -> f64 {
            0.1
        }
        fn default_metric() -> String {
            "cosine".to_string()
        }
        fn default_perplexity() -> f64 {
            30.0
        }

        let req: FitRequest = serde_json::from_value(body)
            .map_err(|e| Status::invalid_argument(format!("Invalid fit request: {}", e)))?;

        if req.embeddings.is_empty() {
            return Err(Status::invalid_argument("embeddings array is empty"));
        }

        let method = req.method.to_lowercase();
        if !KNOWN_METHODS.contains(&method.as_str()) {
            return Err(Status::invalid_argument(format!(
                "Unknown method '{}', expected one of: {}",
                method,
                KNOWN_METHODS.join(", ")
            )));
        }

        let n_points = req.embeddings.len();
        let start = Instant::now();

        let n_components = req.n_components;
        let projections = Python::with_gil(|py| -> PyResult<Vec<Vec<f32>>> {
            let np = py.import("numpy")?;

            // Build numpy array from embeddings
            let inner_lists: Vec<Bound<'_, PyList>> = req
                .embeddings
                .iter()
                .map(|row| PyList::new(py, row.iter()))
                .collect::<PyResult<Vec<_>>>()?;
            let py_list = PyList::new(py, inner_lists.iter())?;
            let np_array = np.call_method1("array", (py_list, "float32"))?;

            let result = match method.as_str() {
                "umap" => {
                    let umap_mod = py.import("umap")?;
                    let kwargs = pyo3::types::PyDict::new(py);
                    kwargs.set_item("n_neighbors", req.n_neighbors)?;
                    kwargs.set_item("min_dist", req.min_dist)?;
                    kwargs.set_item("metric", &req.metric)?;
                    kwargs.set_item("n_components", n_components)?;
                    let reducer = umap_mod.getattr("UMAP")?.call((), Some(&kwargs))?;
                    let result = reducer.call_method1("fit_transform", (np_array,))?;
                    let builtins = py.import("builtins")?;
                    builtins.setattr("_qntx_reduce_model_umap", reducer)?;
                    result
                }
                "tsne" => {
                    let manifold = py.import("sklearn.manifold")?;
                    let kwargs = pyo3::types::PyDict::new(py);
                    kwargs.set_item("n_components", n_components)?;
                    kwargs.set_item("perplexity", req.perplexity)?;
                    let tsne = manifold.getattr("TSNE")?.call((), Some(&kwargs))?;
                    tsne.call_method1("fit_transform", (np_array,))?
                }
                "pca" => {
                    let decomposition = py.import("sklearn.decomposition")?;
                    let kwargs = pyo3::types::PyDict::new(py);
                    kwargs.set_item("n_components", n_components)?;
                    let pca = decomposition.getattr("PCA")?.call((), Some(&kwargs))?;
                    let result = pca.call_method1("fit_transform", (np_array,))?;
                    let builtins = py.import("builtins")?;
                    builtins.setattr("_qntx_reduce_model_pca", pca)?;
                    result
                }
                _ => unreachable!(),
            };

            // Extract projections (2D or 3D depending on n_components)
            let mut projections = Vec::with_capacity(n_points);
            for i in 0..n_points {
                let row = result.call_method1("__getitem__", (i,))?;
                let mut point = Vec::with_capacity(n_components);
                for j in 0..n_components {
                    let v: f32 = row.call_method1("__getitem__", (j,))?.extract()?;
                    point.push(v);
                }
                projections.push(point);
            }
            Ok(projections)
        })
        .map_err(|e| {
            error!("{} fit_transform failed: {}", method, e);
            Status::internal(format!("{} fit_transform failed: {}", method, e))
        })?;

        let fit_ms = start.elapsed().as_millis() as u64;

        // Update state
        {
            let mut state = self.state.write();
            state.fitted.insert(
                method.clone(),
                MethodState {
                    n_points,
                    n_components,
                    supports_transform: method != "tsne",
                },
            );
        }

        info!(
            "{} fit complete: {} points in {}ms",
            method, n_points, fit_ms
        );

        #[derive(Serialize)]
        struct FitResponse {
            method: String,
            n_components: usize,
            projections: Vec<Vec<f32>>,
            n_points: usize,
            fit_ms: u64,
        }

        json_response(
            200,
            &FitResponse {
                method,
                n_components,
                projections,
                n_points,
                fit_ms,
            },
        )
    }

    /// POST /transform — project new points using a fitted model.
    pub fn handle_transform(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
        #[derive(Deserialize)]
        struct TransformRequest {
            embeddings: Vec<Vec<f32>>,
            #[serde(default = "default_method")]
            method: String,
        }

        fn default_method() -> String {
            "umap".to_string()
        }

        let req: TransformRequest = serde_json::from_value(body)
            .map_err(|e| Status::invalid_argument(format!("Invalid transform request: {}", e)))?;

        if req.embeddings.is_empty() {
            return Err(Status::invalid_argument("embeddings array is empty"));
        }

        let method = req.method.to_lowercase();

        if method == "tsne" {
            return Err(Status::failed_precondition(
                "t-SNE does not support transform — re-fit with all points instead",
            ));
        }

        {
            let state = self.state.read();
            if !state.fitted.contains_key(&method) {
                return Err(Status::failed_precondition(format!(
                    "{} model not fitted — call /fit first",
                    method
                )));
            }
        }

        let model_attr = format!("_qntx_reduce_model_{}", method);
        let n_points = req.embeddings.len();
        let start = Instant::now();

        // Get n_components from the fitted model's state
        let n_components = {
            let state = self.state.read();
            state.fitted.get(&method).map_or(2, |s| s.n_components)
        };

        let projections = Python::with_gil(|py| -> PyResult<Vec<Vec<f32>>> {
            let np = py.import("numpy")?;
            let builtins = py.import("builtins")?;

            let reducer = builtins.getattr(model_attr.as_str()).map_err(|_| {
                pyo3::exceptions::PyRuntimeError::new_err(format!("{} model not fitted", method))
            })?;

            let inner_lists: Vec<Bound<'_, PyList>> = req
                .embeddings
                .iter()
                .map(|row| PyList::new(py, row.iter()))
                .collect::<PyResult<Vec<_>>>()?;
            let py_list = PyList::new(py, inner_lists.iter())?;
            let np_array = np.call_method1("array", (py_list, "float32"))?;

            let result = reducer.call_method1("transform", (np_array,))?;

            let mut projections = Vec::with_capacity(n_points);
            for i in 0..n_points {
                let row = result.call_method1("__getitem__", (i,))?;
                let mut point = Vec::with_capacity(n_components);
                for j in 0..n_components {
                    let v: f32 = row.call_method1("__getitem__", (j,))?.extract()?;
                    point.push(v);
                }
                projections.push(point);
            }
            Ok(projections)
        })
        .map_err(|e| {
            error!("{} transform failed: {}", method, e);
            Status::internal(format!("{} transform failed: {}", method, e))
        })?;

        let transform_ms = start.elapsed().as_millis() as u64;

        info!(
            "{} transform complete: {} points in {}ms",
            method, n_points, transform_ms
        );

        #[derive(Serialize)]
        struct TransformResponse {
            method: String,
            n_components: usize,
            projections: Vec<Vec<f32>>,
            n_points: usize,
            transform_ms: u64,
        }

        json_response(
            200,
            &TransformResponse {
                method,
                n_components,
                projections,
                n_points,
                transform_ms,
            },
        )
    }

    /// GET /status — per-method fit status.
    pub fn handle_status(&self) -> Result<HttpResponse, Status> {
        let state = self.state.read();

        #[derive(Serialize)]
        struct MethodStatus {
            fitted: bool,
            n_points: usize,
            n_components: usize,
            supports_transform: bool,
        }

        let mut methods: HashMap<String, MethodStatus> = HashMap::new();
        for &m in KNOWN_METHODS {
            let ms = state.fitted.get(m);
            methods.insert(
                m.to_string(),
                MethodStatus {
                    fitted: ms.is_some(),
                    n_points: ms.map_or(0, |s| s.n_points),
                    n_components: ms.map_or(0, |s| s.n_components),
                    supports_transform: ms.map_or(m != "tsne", |s| s.supports_transform),
                },
            );
        }

        json_response(200, &methods)
    }

    /// Clear all fitted models from Python builtins.
    pub fn clear_models(&self) {
        let mut state = self.state.write();
        state.fitted.clear();

        Python::with_gil(|py| {
            let builtins = match py.import("builtins") {
                Ok(b) => b,
                Err(e) => {
                    warn!("Failed to import builtins for model cleanup: {}", e);
                    return;
                }
            };
            for &m in KNOWN_METHODS {
                let attr = format!("_qntx_reduce_model_{}", m);
                if builtins.getattr(attr.as_str()).is_ok() {
                    let _ = builtins.delattr(attr.as_str());
                }
            }
        });
    }
}

/// Create a JSON HTTP response.
fn json_response<T: Serialize>(status_code: i32, data: &T) -> Result<HttpResponse, Status> {
    let body = serde_json::to_vec(data)
        .map_err(|e| Status::internal(format!("Failed to serialize response: {}", e)))?;

    Ok(HttpResponse {
        status_code,
        headers: vec![HttpHeader {
            name: "Content-Type".to_string(),
            values: vec!["application/json".to_string()],
        }],
        body,
    })
}
