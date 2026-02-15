use crate::proto::{HttpHeader, HttpResponse};
use parking_lot::RwLock;
use pyo3::prelude::*;
use pyo3::types::PyList;
use serde::{Deserialize, Serialize};
use std::sync::Arc;
use std::time::Instant;
use tonic::Status;
use tracing::{error, info};

/// Plugin state holding the fitted UMAP model reference.
pub(crate) struct ReduceState {
    pub fitted: bool,
    pub n_points: usize,
}

/// Handler context wrapping shared state.
pub struct HandlerContext {
    pub(crate) state: Arc<RwLock<ReduceState>>,
}

impl HandlerContext {
    pub(crate) fn new(state: Arc<RwLock<ReduceState>>) -> Self {
        Self { state }
    }

    /// POST /fit — fit UMAP on embeddings and return 2D projections.
    pub fn handle_fit(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
        #[derive(Deserialize)]
        struct FitRequest {
            embeddings: Vec<Vec<f32>>,
            #[serde(default = "default_n_neighbors")]
            n_neighbors: usize,
            #[serde(default = "default_min_dist")]
            min_dist: f64,
            #[serde(default = "default_metric")]
            metric: String,
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

        let req: FitRequest = serde_json::from_value(body)
            .map_err(|e| Status::invalid_argument(format!("Invalid fit request: {}", e)))?;

        if req.embeddings.is_empty() {
            return Err(Status::invalid_argument("embeddings array is empty"));
        }

        let n_points = req.embeddings.len();
        let start = Instant::now();

        let projections = Python::with_gil(|py| -> PyResult<Vec<[f32; 2]>> {
            let np = py.import("numpy")?;
            let umap_mod = py.import("umap")?;

            // Build inner lists, unwrapping each Result, then collect into outer list
            let inner_lists: Vec<Bound<'_, PyList>> = req
                .embeddings
                .iter()
                .map(|row| PyList::new(py, row.iter()))
                .collect::<PyResult<Vec<_>>>()?;
            let py_list = PyList::new(py, inner_lists.iter())?;
            let np_array = np.call_method1("array", (py_list, "float32"))?;

            // Create and fit UMAP
            let kwargs = pyo3::types::PyDict::new(py);
            kwargs.set_item("n_neighbors", req.n_neighbors)?;
            kwargs.set_item("min_dist", req.min_dist)?;
            kwargs.set_item("metric", &req.metric)?;
            kwargs.set_item("n_components", 2)?;
            let reducer = umap_mod.getattr("UMAP")?.call((), Some(&kwargs))?;

            let result = reducer.call_method1("fit_transform", (np_array,))?;

            // Store fitted model in builtins for transform() reuse
            let builtins = py.import("builtins")?;
            builtins.setattr("_qntx_umap_model", reducer)?;

            // Extract 2D projections
            let mut projections = Vec::with_capacity(n_points);
            for i in 0..n_points {
                let row = result.call_method1("__getitem__", (i,))?;
                let x: f32 = row.call_method1("__getitem__", (0,))?.extract()?;
                let y: f32 = row.call_method1("__getitem__", (1,))?.extract()?;
                projections.push([x, y]);
            }
            Ok(projections)
        })
        .map_err(|e| {
            error!("UMAP fit_transform failed: {}", e);
            Status::internal(format!("UMAP fit_transform failed: {}", e))
        })?;

        let fit_ms = start.elapsed().as_millis() as u64;

        // Update state
        {
            let mut state = self.state.write();
            state.fitted = true;
            state.n_points = n_points;
        }

        info!("UMAP fit complete: {} points in {}ms", n_points, fit_ms);

        #[derive(Serialize)]
        struct FitResponse {
            projections: Vec<[f32; 2]>,
            n_points: usize,
            fit_ms: u64,
        }

        json_response(
            200,
            &FitResponse {
                projections,
                n_points,
                fit_ms,
            },
        )
    }

    /// POST /transform — project new points using fitted model.
    pub fn handle_transform(&self, body: serde_json::Value) -> Result<HttpResponse, Status> {
        #[derive(Deserialize)]
        struct TransformRequest {
            embeddings: Vec<Vec<f32>>,
        }

        let req: TransformRequest = serde_json::from_value(body)
            .map_err(|e| Status::invalid_argument(format!("Invalid transform request: {}", e)))?;

        if req.embeddings.is_empty() {
            return Err(Status::invalid_argument("embeddings array is empty"));
        }

        {
            let state = self.state.read();
            if !state.fitted {
                return Err(Status::failed_precondition(
                    "UMAP model not fitted — call /fit first",
                ));
            }
        }

        let n_points = req.embeddings.len();
        let start = Instant::now();

        let projections = Python::with_gil(|py| -> PyResult<Vec<[f32; 2]>> {
            let np = py.import("numpy")?;
            let builtins = py.import("builtins")?;

            let reducer = builtins
                .getattr("_qntx_umap_model")
                .map_err(|_| pyo3::exceptions::PyRuntimeError::new_err("UMAP model not fitted"))?;

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
                let x: f32 = row.call_method1("__getitem__", (0,))?.extract()?;
                let y: f32 = row.call_method1("__getitem__", (1,))?.extract()?;
                projections.push([x, y]);
            }
            Ok(projections)
        })
        .map_err(|e| {
            error!("UMAP transform failed: {}", e);
            Status::internal(format!("UMAP transform failed: {}", e))
        })?;

        let transform_ms = start.elapsed().as_millis() as u64;

        info!(
            "UMAP transform complete: {} points in {}ms",
            n_points, transform_ms
        );

        #[derive(Serialize)]
        struct TransformResponse {
            projections: Vec<[f32; 2]>,
            n_points: usize,
            transform_ms: u64,
        }

        json_response(
            200,
            &TransformResponse {
                projections,
                n_points,
                transform_ms,
            },
        )
    }

    /// GET /status — whether the model is fitted and how many points.
    pub fn handle_status(&self) -> Result<HttpResponse, Status> {
        let state = self.state.read();

        #[derive(Serialize)]
        struct StatusResponse {
            fitted: bool,
            n_points: usize,
        }

        json_response(
            200,
            &StatusResponse {
                fitted: state.fitted,
                n_points: state.n_points,
            },
        )
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
