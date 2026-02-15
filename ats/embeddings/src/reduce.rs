/// Contract for dimensionality reduction backends.
///
/// Current implementation: Python umap-learn via qntx-reduce plugin (gRPC).
/// This trait exists so a future native Rust backend can be swapped in.
pub trait DimensionReducer {
    /// Fit the model on `data` and return 2D projections for all points.
    fn fit_transform(
        &mut self,
        data: &[f32],
        n_points: usize,
        input_dims: usize,
    ) -> Result<Vec<[f32; 2]>, String>;

    /// Project new points using an already-fitted model.
    fn transform(
        &self,
        data: &[f32],
        n_points: usize,
        input_dims: usize,
    ) -> Result<Vec<[f32; 2]>, String>;

    /// Whether the model has been fitted.
    fn is_fitted(&self) -> bool;
}
