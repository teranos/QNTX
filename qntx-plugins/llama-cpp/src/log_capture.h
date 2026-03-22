#pragma once

#include <iostream>
#include <mutex>
#include <string>
#include <vector>

#include "llama.h"

// Captures llama.cpp log output and condenses it into a summary.
// Replaces 1000+ lines of stderr noise with a few informational lines on stdout.
class LogCapture {
public:
    static LogCapture& instance() {
        static LogCapture inst;
        return inst;
    }

    // Install as llama.cpp log callback. Call before llama_backend_init().
    void install() {
        llama_log_set(log_callback, this);
    }

    // Flush condensed summary to stdout. Call after model loading completes.
    void flush_summary(const std::string& log_level) {
        std::lock_guard<std::mutex> lock(mutex_);

        // Always show actual errors
        for (auto& line : error_lines_) {
            std::cout << "[llama-cpp] ERROR: " << line << std::endl;
        }

        if (log_level == "error") return;
        if (log_level == "warn") return;

        // info: 3 condensed lines
        // Line 1: Metal device
        if (!device_name_.empty()) {
            std::cout << "[llama-cpp] Metal: " << device_name_;
            if (!gpu_family_.empty()) std::cout << " (" << gpu_family_ << ")";
            if (vram_free_mib_ > 0) std::cout << ", " << vram_free_mib_ << " MiB free";
            std::string caps;
            if (has_unified_memory_) caps += "unified";
            if (has_bfloat_) { if (!caps.empty()) caps += ", "; caps += "bfloat"; }
            if (has_flash_attn_) { if (!caps.empty()) caps += ", "; caps += "flash-attn"; }
            if (!caps.empty()) std::cout << ", " << caps;
            std::cout << std::endl;
        }

        // Line 2: Buffers
        if (kv_buffer_mib_ > 0 || compute_buffer_mib_ > 0) {
            std::cout << "[llama-cpp] Buffers: KV " << kv_buffer_mib_ << " MiB ("
                      << n_layers_ << " layers), compute " << compute_buffer_mib_ << " MiB";
            if (cpu_buffer_mib_ > 0) std::cout << " + " << cpu_buffer_mib_ << " MiB (CPU)";
            std::cout << std::endl;
        }

        // Line 3: Model
        if (!model_name_.empty()) {
            std::cout << "[llama-cpp] Model: " << model_name_;
            if (!quant_type_.empty()) std::cout << ", " << quant_type_;
            if (!file_size_.empty()) std::cout << ", " << file_size_;
            std::cout << std::endl;
        }

        if (log_level != "debug") return;

        // debug: additional detail
        if (metal_load_secs_ > 0) {
            std::cout << "[llama-cpp] Metal library loaded in " << metal_load_secs_ << " sec" << std::endl;
        }
        if (graph_nodes_ > 0) {
            std::cout << "[llama-cpp] Graph: " << graph_nodes_ << " nodes, "
                      << graph_splits_ << " splits" << std::endl;
        }
        if (reserve_ms_ > 0) {
            std::cout << "[llama-cpp] Scheduler reserve took " << reserve_ms_ << " ms" << std::endl;
        }
        if (n_tensors_ > 0) {
            std::cout << "[llama-cpp] Tensors: " << n_tensors_ << " total" << std::endl;
        }
    }

private:
    LogCapture() = default;

    static void log_callback(ggml_log_level level, const char* text, void* user_data) {
        auto* self = static_cast<LogCapture*>(user_data);
        self->capture(level, text);
    }

    void capture(ggml_log_level level, const char* text) {
        std::lock_guard<std::mutex> lock(mutex_);

        // Accumulate partial lines (CONT level or no newline)
        line_buf_ += text;
        if (line_buf_.empty() || line_buf_.back() != '\n') return;

        // Strip trailing newline
        std::string line = line_buf_.substr(0, line_buf_.size() - 1);
        line_buf_.clear();

        if (line.empty()) return;

        if (level == GGML_LOG_LEVEL_ERROR) {
            error_lines_.push_back(line);
            return;
        }

        // Extract key values from known line patterns

        // Device: "using device MTL0 (Apple M1) (unknown id) - 5460 MiB free"
        if (line.find("using device") != std::string::npos) {
            auto paren1 = line.find('(');
            auto paren2 = line.find(')', paren1 + 1);
            if (paren1 != std::string::npos && paren2 != std::string::npos) {
                device_name_ = line.substr(paren1 + 1, paren2 - paren1 - 1);
            }
            auto free_pos = line.find("MiB free");
            if (free_pos != std::string::npos) {
                // Walk back from "MiB free" to find the number
                auto dash = line.rfind("- ", free_pos);
                if (dash != std::string::npos) {
                    auto num_str = trim(line.substr(dash + 2, free_pos - dash - 2));
                    try { vram_free_mib_ = std::stoi(num_str); } catch (...) {}
                }
            }
        }
        // GPU family (first one only): "GPU family: MTLGPUFamilyApple7  (1007)"
        else if (line.find("GPU family:") != std::string::npos && gpu_family_.empty()) {
            auto pos = line.find("GPU family:");
            gpu_family_ = trim(line.substr(pos + 11));
        }
        // Model name: "general.name str = Llama 3.2 3B Instruct"
        else if (line.find("general.name") != std::string::npos && line.find("str") != std::string::npos) {
            auto eq = line.find('=');
            if (eq != std::string::npos) {
                model_name_ = trim(line.substr(eq + 1));
            }
        }
        // File size: "file size   = 1.87 GiB (5.01 BPW)"
        else if (line.find("file size") != std::string::npos && line.find("GiB") != std::string::npos) {
            auto eq = line.find('=');
            if (eq != std::string::npos) {
                file_size_ = trim(line.substr(eq + 1));
            }
        }
        // Quant type: "file type   = Q4_K - Medium"
        else if (line.find("file type") != std::string::npos && line.find("print_info") != std::string::npos) {
            auto eq = line.find('=');
            if (eq != std::string::npos) {
                quant_type_ = trim(line.substr(eq + 1));
            }
        }
        // Capabilities
        else if (line.find("has unified memory") != std::string::npos && line.find("true") != std::string::npos) {
            has_unified_memory_ = true;
        }
        else if (line.find("has bfloat") != std::string::npos && line.find("true") != std::string::npos) {
            has_bfloat_ = true;
        }
        else if (line.find("Flash Attention") != std::string::npos && line.find("enabled") != std::string::npos) {
            has_flash_attn_ = true;
        }
        // KV cache: "llama_kv_cache:       MTL0 KV buffer size =   224.00 MiB"
        else if (line.find("KV buffer size") != std::string::npos) {
            auto eq = line.find('=');
            if (eq != std::string::npos) {
                auto num_str = trim(line.substr(eq + 1));
                try { kv_buffer_mib_ = std::stof(num_str); } catch (...) {}
            }
        }
        // Compute buffer: "sched_reserve:       MTL0 compute buffer size =   262.50 MiB"
        else if (line.find("compute buffer size") != std::string::npos) {
            auto eq = line.find('=');
            if (eq != std::string::npos) {
                auto num_str = trim(line.substr(eq + 1));
                float val = 0;
                try { val = std::stof(num_str); } catch (...) {}
                if (line.find("CPU") != std::string::npos) {
                    cpu_buffer_mib_ = val;
                } else {
                    compute_buffer_mib_ = val;
                }
            }
        }
        // Per-layer KV cache lines — just count
        else if (line.find("llama_kv_cache: layer") != std::string::npos) {
            n_layers_++;
        }
        // Metal library load: "ggml_metal_library_init: loaded in 0.066 sec"
        else if (line.find("loaded in") != std::string::npos && line.find("metal_library") != std::string::npos) {
            auto pos = line.find("loaded in");
            auto num_str = trim(line.substr(pos + 9));
            try { metal_load_secs_ = std::stof(num_str); } catch (...) {}
        }
        // Graph: "graph nodes  = 875"
        else if (line.find("graph nodes") != std::string::npos) {
            auto eq = line.find('=');
            if (eq != std::string::npos) {
                try { graph_nodes_ = std::stoi(trim(line.substr(eq + 1))); } catch (...) {}
            }
        }
        // Graph splits: "graph splits = 2"
        else if (line.find("graph splits") != std::string::npos) {
            auto eq = line.find('=');
            if (eq != std::string::npos) {
                try { graph_splits_ = std::stoi(trim(line.substr(eq + 1))); } catch (...) {}
            }
        }
        // Reserve time: "reserve took 8.86 ms"
        else if (line.find("reserve took") != std::string::npos) {
            auto pos = line.find("reserve took");
            auto num_str = trim(line.substr(pos + 12));
            try { reserve_ms_ = std::stof(num_str); } catch (...) {}
        }
        // Tensor count: "loaded meta data with 30 key-value pairs and 255 tensors"
        else if (line.find("tensors from") != std::string::npos) {
            auto pos = line.find("and ");
            if (pos != std::string::npos) {
                try { n_tensors_ = std::stoi(line.substr(pos + 4)); } catch (...) {}
            }
        }
        // All other lines silently dropped
    }

    static std::string trim(const std::string& s) {
        auto start = s.find_first_not_of(" \t");
        if (start == std::string::npos) return "";
        auto end = s.find_last_not_of(" \t");
        return s.substr(start, end - start + 1);
    }

    std::mutex mutex_;
    std::string line_buf_;

    // Extracted values
    std::string device_name_;
    std::string gpu_family_;
    int vram_free_mib_ = 0;
    bool has_unified_memory_ = false;
    bool has_bfloat_ = false;
    bool has_flash_attn_ = false;
    std::string model_name_;
    std::string quant_type_;
    std::string file_size_;
    float kv_buffer_mib_ = 0;
    float compute_buffer_mib_ = 0;
    float cpu_buffer_mib_ = 0;
    int n_layers_ = 0;
    float metal_load_secs_ = 0;
    int graph_nodes_ = 0;
    int graph_splits_ = 0;
    float reserve_ms_ = 0;
    int n_tensors_ = 0;

    std::vector<std::string> error_lines_;
};
