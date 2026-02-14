use hdbscan::{Center, Hdbscan, HdbscanHyperParams};

pub struct ClusterResult {
    pub labels: Vec<i32>,
    pub probabilities: Vec<f32>,
    pub n_clusters: usize,
    pub centroids: Vec<Vec<f32>>, // one centroid per cluster, indexed by label
}

/// Cluster embedding vectors using HDBSCAN.
///
/// `data` is a flat f32 array of n_points * dimensions.
/// Returns cluster labels per point (-1 = noise) and membership probabilities.
pub fn cluster_embeddings(
    data: &[f32],
    n_points: usize,
    dimensions: usize,
    min_cluster_size: usize,
) -> Result<ClusterResult, String> {
    if data.len() != n_points * dimensions {
        return Err(format!(
            "data length {} != n_points {} * dimensions {}",
            data.len(),
            n_points,
            dimensions
        ));
    }

    if n_points < 2 {
        return Err(format!("need at least 2 points, got {}", n_points));
    }

    // Reshape flat f32 into Vec<Vec<f64>> (hdbscan expects f64)
    let points: Vec<Vec<f64>> = (0..n_points)
        .map(|i| {
            let start = i * dimensions;
            data[start..start + dimensions]
                .iter()
                .map(|&v| v as f64)
                .collect()
        })
        .collect();

    let params = HdbscanHyperParams::builder()
        .min_cluster_size(min_cluster_size)
        .build();

    let clusterer = Hdbscan::new(&points, params);
    let labels = clusterer
        .cluster()
        .map_err(|e| format!("HDBSCAN failed: {:?}", e))?;

    // Count distinct clusters (labels >= 0)
    let n_clusters = {
        let mut seen = std::collections::HashSet::new();
        for &l in &labels {
            if l >= 0 {
                seen.insert(l);
            }
        }
        seen.len()
    };

    // Phase 1: binary probabilities (1.0 = clustered, 0.0 = noise)
    let probabilities: Vec<f32> = labels
        .iter()
        .map(|&l| if l >= 0 { 1.0 } else { 0.0 })
        .collect();

    // Compute cluster centroids (mean of each cluster's points)
    let centroids: Vec<Vec<f32>> = if n_clusters > 0 {
        let centers_f64 = clusterer
            .calc_centers(Center::Centroid, &labels)
            .map_err(|e| format!("centroid computation failed: {:?}", e))?;
        centers_f64
            .into_iter()
            .map(|c| c.into_iter().map(|v| v as f32).collect())
            .collect()
    } else {
        Vec::new()
    };

    Ok(ClusterResult {
        labels,
        probabilities,
        n_clusters,
        centroids,
    })
}

#[cfg(test)]
mod tests {
    use super::*;

    #[test]
    fn test_cluster_synthetic_blobs() {
        // Two well-separated clusters of 10 points each in 2D
        let mut data = Vec::new();
        for i in 0..10 {
            data.push(0.0 + (i as f32) * 0.01);
            data.push(0.0 + (i as f32) * 0.01);
        }
        for i in 0..10 {
            data.push(10.0 + (i as f32) * 0.01);
            data.push(10.0 + (i as f32) * 0.01);
        }

        let result = cluster_embeddings(&data, 20, 2, 3).unwrap();
        assert_eq!(result.labels.len(), 20);
        assert_eq!(result.probabilities.len(), 20);
        // With well-separated blobs, should find 2 clusters
        assert_eq!(
            result.n_clusters, 2,
            "expected 2 clusters, got {}",
            result.n_clusters
        );
        // Should have 2 centroids, each with 2 dimensions
        assert_eq!(result.centroids.len(), 2, "expected 2 centroids");
        for (i, centroid) in result.centroids.iter().enumerate() {
            assert_eq!(centroid.len(), 2, "centroid {} should have 2 dimensions", i);
        }
    }

    #[test]
    fn test_cluster_validates_data_length() {
        let data = vec![1.0, 2.0, 3.0];
        let result = cluster_embeddings(&data, 2, 2, 3);
        assert!(result.is_err());
    }

    #[test]
    fn test_cluster_too_few_points() {
        let data = vec![1.0, 2.0];
        let result = cluster_embeddings(&data, 1, 2, 3);
        assert!(result.is_err());
    }
}
