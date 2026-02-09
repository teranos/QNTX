use qntx_embeddings::EmbeddingEngine;

/// Calculate cosine similarity between two vectors
fn cosine_similarity(a: &[f32], b: &[f32]) -> f32 {
    let dot_product: f32 = a.iter().zip(b.iter()).map(|(x, y)| x * y).sum();
    let magnitude_a: f32 = a.iter().map(|x| x * x).sum::<f32>().sqrt();
    let magnitude_b: f32 = b.iter().map(|x| x * x).sum::<f32>().sqrt();

    if magnitude_a * magnitude_b == 0.0 {
        0.0
    } else {
        dot_product / (magnitude_a * magnitude_b)
    }
}

fn main() -> anyhow::Result<()> {
    println!("Testing semantic similarity with real tokenization...\n");

    let model_path = "models/all-MiniLM-L6-v2/model.onnx";
    let mut engine = EmbeddingEngine::new(model_path, "all-MiniLM-L6-v2".to_string())?;

    // Test similar sentences
    let sentences = vec![
        ("cat", "kitten", "Should be very similar (cat/kitten)"),
        ("dog", "puppy", "Should be very similar (dog/puppy)"),
        ("cat", "dog", "Should be somewhat similar (both animals)"),
        ("cat", "car", "Should be less similar (animal vs vehicle)"),
        ("happy", "joyful", "Should be very similar (synonyms)"),
        ("happy", "sad", "Should be opposite (antonyms)"),
        (
            "The weather is nice today",
            "It's a beautiful day",
            "Should be similar (same meaning)",
        ),
        (
            "The weather is nice today",
            "The stock market crashed",
            "Should be dissimilar (unrelated)",
        ),
    ];

    println!("Generating embeddings and calculating similarities...\n");

    for (sent1, sent2, description) in sentences {
        let emb1 = engine.embed(sent1)?;
        let emb2 = engine.embed(sent2)?;

        let similarity = cosine_similarity(&emb1.embedding, &emb2.embedding);

        println!("{}", description);
        println!("  '{}' vs '{}'", sent1, sent2);
        println!("  Similarity: {:.4}", similarity);
        println!("  Tokens: {} vs {}", emb1.tokens, emb2.tokens);
        println!();
    }

    println!("✅ Semantic similarity test complete!");

    // Additional test: batch similar words and check their clustering
    println!("\n--- Testing word clustering ---\n");

    let word_groups = vec![
        vec!["car", "automobile", "vehicle", "truck"],
        vec!["happy", "joyful", "cheerful", "glad"],
        vec!["computer", "laptop", "desktop", "PC"],
    ];

    for group in word_groups {
        println!("Group: {:?}", group);
        let mut embeddings = Vec::new();

        for word in &group {
            let emb = engine.embed(word)?;
            embeddings.push(emb.embedding);
        }

        // Calculate average similarity within group
        let mut total_similarity = 0.0;
        let mut count = 0;

        for i in 0..embeddings.len() {
            for j in i + 1..embeddings.len() {
                let sim = cosine_similarity(&embeddings[i], &embeddings[j]);
                total_similarity += sim;
                count += 1;
                println!("  {} vs {}: {:.4}", group[i], group[j], sim);
            }
        }

        if count > 0 {
            println!(
                "  Average similarity: {:.4}\n",
                total_similarity / count as f32
            );
        }
    }

    Ok(())
}
