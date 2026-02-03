use qntx_embeddings::EmbeddingEngine;

fn main() -> anyhow::Result<()> {
    println!("Loading ONNX model...");

    let model_path = "../models/all-MiniLM-L6-v2/model.onnx";
    let mut engine = EmbeddingEngine::new(model_path, "all-MiniLM-L6-v2".to_string())?;

    println!("Model loaded successfully!");
    println!("Model info: {:?}", engine.model_info());

    // Test embedding a simple sentence
    let test_text = "Hello, this is a test sentence for embeddings.";
    println!("\nGenerating embedding for: \"{}\"", test_text);

    let result = engine.embed(test_text)?;

    println!("Embedding generated!");
    println!("  Dimensions: {}", result.embedding.len());
    println!("  First 10 values: {:?}", &result.embedding[..10.min(result.embedding.len())]);
    println!("  Inference time: {:.2}ms", result.inference_ms);
    println!("  Token count: {}", result.tokens);

    // Check if we're getting real embeddings (not all zeros or dummy values)
    let non_zero_count = result.embedding.iter().filter(|&&x| x != 0.0 && x != 0.1).count();
    println!("\nNon-zero/non-dummy values: {}/{}", non_zero_count, result.embedding.len());

    if non_zero_count > 0 {
        println!("✅ SUCCESS: Real embeddings are being generated!");
    } else {
        println!("⚠️  WARNING: Embeddings might still be dummy values");
    }

    Ok(())
}