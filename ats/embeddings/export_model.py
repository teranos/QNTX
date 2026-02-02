#!/usr/bin/env python3
"""
Export a HuggingFace sentence-transformer model to ONNX format for use with QNTX embeddings.

Usage:
    python export_model.py [model_name] [output_dir]

Example:
    python export_model.py sentence-transformers/all-MiniLM-L6-v2 models/all-MiniLM-L6-v2
    python export_model.py  # Uses defaults
"""

import os
import sys
from pathlib import Path

def export_model(model_name="sentence-transformers/all-MiniLM-L6-v2", output_dir="models/all-MiniLM-L6-v2"):
    """Export a HuggingFace model to ONNX format."""

    print(f"📦 Exporting model: {model_name}")
    print(f"📁 Output directory: {output_dir}")

    # Try to import required packages
    try:
        from optimum.onnxruntime import ORTModelForFeatureExtraction
        from transformers import AutoTokenizer
    except ImportError:
        print("\n❌ Missing dependencies. Please install:")
        print("   pip install transformers optimum[onnxruntime]")
        sys.exit(1)

    # Create output directory
    output_path = Path(output_dir)
    output_path.mkdir(parents=True, exist_ok=True)

    print("\n⏳ Loading and exporting model to ONNX...")
    try:
        # Export model to ONNX
        model = ORTModelForFeatureExtraction.from_pretrained(
            model_name,
            export=True,
            provider="CPUExecutionProvider"
        )
        model.save_pretrained(output_path)
        print(f"✅ Model exported to: {output_path}/model.onnx")

        # Also save the tokenizer for future use
        print("\n⏳ Saving tokenizer...")
        tokenizer = AutoTokenizer.from_pretrained(model_name)
        tokenizer.save_pretrained(output_path)
        print(f"✅ Tokenizer saved to: {output_path}")

        # Get model info
        print("\n📊 Model Information:")
        print(f"   Model name: {model_name}")

        # Check file sizes
        model_file = output_path / "model.onnx"
        if model_file.exists():
            size_mb = model_file.stat().st_size / (1024 * 1024)
            print(f"   ONNX size: {size_mb:.1f} MB")

        # Test the model
        print("\n🧪 Testing model...")
        test_text = "This is a test sentence for embedding."
        inputs = tokenizer(test_text, return_tensors="np", padding=True, truncation=True)
        outputs = model(**inputs)

        # Get embeddings (mean pooling over tokens)
        embeddings = outputs.last_hidden_state.mean(axis=1)
        print(f"   Test embedding shape: {embeddings.shape}")
        print(f"   Embedding dimensions: {embeddings.shape[-1]}")

        print("\n✅ Export complete! Model is ready for use with QNTX.")
        print(f"\nTo use in QNTX:")
        print(f"   service.Init(\"{output_path}/model.onnx\")")

    except Exception as e:
        print(f"\n❌ Error during export: {e}")
        sys.exit(1)

def main():
    """Main entry point."""
    model_name = "sentence-transformers/all-MiniLM-L6-v2"
    output_dir = "models/all-MiniLM-L6-v2"

    # Parse command line arguments
    if len(sys.argv) > 1:
        model_name = sys.argv[1]
    if len(sys.argv) > 2:
        output_dir = sys.argv[2]

    # If output dir not specified, derive from model name
    if len(sys.argv) == 2:
        model_base = model_name.split("/")[-1]
        output_dir = f"models/{model_base}"

    export_model(model_name, output_dir)

if __name__ == "__main__":
    main()