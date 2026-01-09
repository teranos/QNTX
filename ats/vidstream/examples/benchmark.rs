use qntx_vidstream::types::VideoEngineConfig;
use qntx_vidstream::{FrameFormat, VideoEngine};
use std::time::Instant;

fn main() {
    let model_path = "models/yolo11n.onnx";

    let config = VideoEngineConfig {
        model_path: model_path.to_string(),
        confidence_threshold: 0.25,
        nms_threshold: 0.45,
        input_width: 640,
        input_height: 640,
        num_threads: 4,
        use_gpu: false,
        labels: None,
    };

    println!("Creating video engine with model: {}", model_path);
    let engine = match VideoEngine::new(config) {
        Ok(e) => e,
        Err(e) => {
            eprintln!("Failed to create engine: {}", e);
            std::process::exit(1);
        }
    };

    println!("Engine ready: {}", engine.is_ready());
    if !engine.is_ready() {
        eprintln!("Engine not ready - ONNX feature may not be enabled");
        std::process::exit(1);
    }

    let (width, height) = engine.input_dimensions();
    println!("Model input dimensions: {}x{}", width, height);

    // Create test frame (640x480, RGB8)
    let test_width = 640u32;
    let test_height = 480u32;
    let frame_size = (test_width * test_height * 3) as usize;
    let mut frame_data = vec![0u8; frame_size];

    // Fill with gradient pattern for more interesting input
    for y in 0..test_height {
        for x in 0..test_width {
            let idx = ((y * test_width + x) * 3) as usize;
            frame_data[idx] = (x % 256) as u8;     // R
            frame_data[idx + 1] = (y % 256) as u8; // G
            frame_data[idx + 2] = 128;              // B
        }
    }

    println!("\nRunning warmup pass...");
    let _ = engine.process_frame(&frame_data, test_width, test_height, FrameFormat::RGB8, 0);

    println!("\nBenchmarking {} frames at {}x{}...", 10, test_width, test_height);
    let mut total_time = 0u128;
    let iterations = 10;

    for i in 0..iterations {
        let start = Instant::now();
        let (detections, stats) = engine.process_frame(
            &frame_data,
            test_width,
            test_height,
            FrameFormat::RGB8,
            i as u64,
        );
        let elapsed = start.elapsed();
        total_time += elapsed.as_micros();

        println!("\nFrame {}: {:?}", i, elapsed);
        println!("  Preprocess:  {:6} μs", stats.preprocess_us);
        println!("  Inference:   {:6} μs", stats.inference_us);
        println!("  Postprocess: {:6} μs", stats.postprocess_us);
        println!("  Total:       {:6} μs", stats.total_us);
        println!("  Detections:  {} (raw: {})", stats.detections_final, stats.detections_raw);

        if !detections.is_empty() {
            println!("  First detection: {:?}", detections[0]);
        }
    }

    let avg_time = total_time / iterations;
    let fps = 1_000_000.0 / avg_time as f64;

    println!("\n=== Results ===");
    println!("Average latency: {} μs ({:.2} ms)", avg_time, avg_time as f64 / 1000.0);
    println!("Throughput: {:.1} FPS", fps);
}
