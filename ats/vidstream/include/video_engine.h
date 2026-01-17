/**
 * QNTX Video Engine - C API
 *
 * Real-time video processing library for QNTX attestation generation.
 * This header provides the C interface for integration with Go via CGO.
 *
 * Memory Ownership Rules:
 * - video_engine_new() returns a pointer owned by the caller
 * - video_engine_free() must be called to deallocate the engine
 * - Result structs contain owned pointers that must be freed
 * - Use video_result_free() to deallocate processing results
 *
 * Thread Safety:
 * - VideoEngine is internally thread-safe
 * - Multiple threads can call process_frame concurrently
 */

#ifndef QNTX_VIDEO_ENGINE_H
#define QNTX_VIDEO_ENGINE_H

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#ifdef __cplusplus
extern "C" {
#endif

/**
 * Opaque pointer to Rust VideoEngine.
 * Do not dereference - only pass to video_engine_* functions.
 */
typedef struct VideoEngine VideoEngine;

/**
 * Pixel format for input frames.
 */
typedef enum {
    VIDEO_FORMAT_RGB8 = 0,   /* RGB with 8 bits per channel (24 bpp) */
    VIDEO_FORMAT_RGBA8 = 1,  /* RGBA with 8 bits per channel (32 bpp) */
    VIDEO_FORMAT_BGR8 = 2,   /* BGR with 8 bits per channel (OpenCV default) */
    VIDEO_FORMAT_YUV420 = 3, /* YUV420 planar (common video format) */
    VIDEO_FORMAT_GRAY8 = 4,  /* Grayscale 8-bit */
} VideoFormat;

/**
 * Bounding box for detected objects.
 */
typedef struct {
    float x;      /* X coordinate of top-left corner (pixels) */
    float y;      /* Y coordinate of top-left corner (pixels) */
    float width;  /* Width of bounding box (pixels) */
    float height; /* Height of bounding box (pixels) */
} BoundingBoxC;

/**
 * A single detection result.
 * The label string is owned and must be freed.
 */
typedef struct {
    uint32_t class_id;   /* Class/label ID */
    char *label;         /* Human-readable label (owned) */
    float confidence;    /* Confidence score 0.0-1.0 */
    BoundingBoxC bbox;   /* Bounding box */
    uint64_t track_id;   /* Track ID (0 if not tracked) */
} DetectionC;

/**
 * Processing statistics for performance monitoring.
 */
typedef struct {
    uint64_t preprocess_us;   /* Frame preprocessing time (microseconds) */
    uint64_t inference_us;    /* Model inference time (microseconds) */
    uint64_t postprocess_us;  /* Post-processing/NMS time (microseconds) */
    uint64_t total_us;        /* Total processing time (microseconds) */
    uint32_t frame_width;     /* Frame width processed */
    uint32_t frame_height;    /* Frame height processed */
    uint32_t detections_raw;  /* Detections before NMS */
    uint32_t detections_final;/* Detections after NMS */
} ProcessingStatsC;

/**
 * Result of frame processing.
 * Must be freed with video_result_free().
 */
typedef struct {
    bool success;              /* True if operation succeeded */
    char *error_msg;           /* Error message if !success (owned) */
    DetectionC *detections;    /* Array of detections (owned) */
    size_t detections_len;     /* Number of detections */
    ProcessingStatsC stats;    /* Processing statistics */
} VideoResultC;

/**
 * Engine configuration.
 */
typedef struct {
    const char *model_path;      /* Path to ONNX model file */
    float confidence_threshold;  /* Confidence threshold 0.0-1.0 */
    float nms_threshold;         /* NMS IoU threshold 0.0-1.0 */
    uint32_t input_width;        /* Model input width (0 for auto) */
    uint32_t input_height;       /* Model input height (0 for auto) */
    uint32_t num_threads;        /* Inference threads (0 for auto) */
    bool use_gpu;                /* Enable GPU inference */
    const char *labels;          /* Class labels (optional) */
} VideoEngineConfigC;

/* ============================================================================
 * Engine Lifecycle
 * ============================================================================ */

/**
 * Create a new VideoEngine with default configuration.
 *
 * @return Pointer to new engine, or NULL on failure.
 *         Caller owns the pointer and must call video_engine_free().
 */
VideoEngine *video_engine_new(void);

/**
 * Create a new VideoEngine with custom configuration.
 *
 * @param config Engine configuration.
 * @return Pointer to new engine, or NULL on failure.
 */
VideoEngine *video_engine_new_with_config(const VideoEngineConfigC *config);

/**
 * Free a VideoEngine instance.
 *
 * @param engine Engine to free (safe to pass NULL).
 */
void video_engine_free(VideoEngine *engine);

/* ============================================================================
 * Frame Processing
 * ============================================================================ */

/**
 * Process a single video frame and return detections.
 *
 * @param engine Valid engine pointer.
 * @param frame_data Pointer to raw pixel data.
 * @param frame_len Length of frame data in bytes.
 * @param width Frame width in pixels.
 * @param height Frame height in pixels.
 * @param format Pixel format (VIDEO_FORMAT_*).
 * @param timestamp_us Frame timestamp in microseconds.
 * @return Result struct. Must free with video_result_free().
 */
VideoResultC video_engine_process_frame(
    const VideoEngine *engine,
    const uint8_t *frame_data,
    size_t frame_len,
    uint32_t width,
    uint32_t height,
    int format,
    uint64_t timestamp_us
);

/**
 * Free a VideoResultC and all contained data.
 *
 * @param result Result to free.
 */
void video_result_free(VideoResultC result);

/* ============================================================================
 * Utilities
 * ============================================================================ */

/**
 * Check if the engine is ready for inference.
 *
 * @param engine Engine pointer.
 * @return True if ready.
 */
bool video_engine_is_ready(const VideoEngine *engine);

/**
 * Get the model input dimensions.
 *
 * @param engine Valid engine pointer.
 * @param width Pointer to receive width.
 * @param height Pointer to receive height.
 * @return True if successful.
 */
bool video_engine_get_input_dimensions(
    const VideoEngine *engine,
    uint32_t *width,
    uint32_t *height
);

/**
 * Get the expected frame size for given dimensions and format.
 *
 * @param width Frame width.
 * @param height Frame height.
 * @param format Pixel format.
 * @return Expected size in bytes, or 0 for invalid format.
 */
size_t video_expected_frame_size(uint32_t width, uint32_t height, int format);

/**
 * Free a string returned by FFI functions.
 *
 * @param s String to free (safe to pass NULL).
 */
void video_string_free(char *s);

/**
 * Get the vidstream library version string.
 *
 * @return Version string (e.g., "0.1.0"). Do not free - points to static memory.
 */
const char *video_engine_version(void);

#ifdef __cplusplus
}
#endif

#endif /* QNTX_VIDEO_ENGINE_H */
