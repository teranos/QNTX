/// Cosine similarity between two f32 slices.
///
/// Returns 0.0 if either vector has zero magnitude.
/// Panics if slices have different lengths.
pub fn cosine_similarity(a: &[f32], b: &[f32]) -> f32 {
    assert_eq!(
        a.len(),
        b.len(),
        "vector dimension mismatch: {} vs {}",
        a.len(),
        b.len()
    );

    let mut dot = 0.0f32;
    let mut norm_a = 0.0f32;
    let mut norm_b = 0.0f32;

    for i in 0..a.len() {
        dot += a[i] * b[i];
        norm_a += a[i] * a[i];
        norm_b += b[i] * b[i];
    }

    let denom = norm_a.sqrt() * norm_b.sqrt();
    if denom == 0.0 {
        return 0.0;
    }

    dot / denom
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn identical_vectors() {
        let v = vec![1.0, 2.0, 3.0];
        let sim = cosine_similarity(&v, &v);
        assert!(
            (sim - 1.0).abs() < 1e-6,
            "identical vectors should have similarity ~1.0, got {}",
            sim
        );
    }

    #[test]
    fn orthogonal_vectors() {
        let a = vec![1.0, 0.0, 0.0];
        let b = vec![0.0, 1.0, 0.0];
        let sim = cosine_similarity(&a, &b);
        assert!(
            sim.abs() < 1e-6,
            "orthogonal vectors should have similarity ~0.0, got {}",
            sim
        );
    }

    #[test]
    fn opposite_vectors() {
        let a = vec![1.0, 2.0, 3.0];
        let b = vec![-1.0, -2.0, -3.0];
        let sim = cosine_similarity(&a, &b);
        assert!(
            (sim + 1.0).abs() < 1e-6,
            "opposite vectors should have similarity ~-1.0, got {}",
            sim
        );
    }

    #[test]
    fn zero_vector() {
        let a = vec![1.0, 2.0, 3.0];
        let b = vec![0.0, 0.0, 0.0];
        let sim = cosine_similarity(&a, &b);
        assert_eq!(sim, 0.0, "zero vector should yield similarity 0.0");
    }

    #[test]
    #[should_panic(expected = "vector dimension mismatch")]
    fn dimension_mismatch() {
        cosine_similarity(&[1.0, 2.0], &[1.0, 2.0, 3.0]);
    }

    #[test]
    fn known_similarity() {
        let a = vec![1.0, 0.0];
        let b = vec![1.0, 1.0];
        let sim = cosine_similarity(&a, &b);
        // cos(45°) = 1/√2 ≈ 0.7071
        assert!((sim - std::f32::consts::FRAC_1_SQRT_2).abs() < 1e-6);
    }
}
