#include "pdf_extract.h"

#include <cstdlib>
#include <cstring>
#include <iostream>

// MuPDF is a C library — this header pulls in the entire fitz API:
// contexts, streams, documents, structured text, output buffers.
#include <mupdf/fitz.h>

std::string extract_pdf_text(const uint8_t* data, size_t len) {
    // fz_context is MuPDF's "world" object — allocator, error jmp_buf, caches.
    // FZ_STORE_DEFAULT = 256 MB cache for decoded font/image data.
    // Not thread-safe: one context per thread (or per call, as we do here).
    fz_context* ctx = fz_new_context(nullptr, nullptr, FZ_STORE_DEFAULT);
    if (!ctx) {
        std::cout << "[pdf] Failed to create MuPDF context" << std::endl;
        return "";
    }

    // Register handlers for PDF, EPUB, etc. Without this, fz_open_document
    // doesn't know how to parse any format.
    fz_register_document_handlers(ctx);

    // Declare all MuPDF resources before fz_try — see header comment about
    // why we can't declare them inside the try block (longjmp + destructors).
    fz_stream* stream = nullptr;
    fz_document* doc = nullptr;
    fz_buffer* buf = nullptr;
    fz_output* out = nullptr;
    std::string result;

    // fz_try/fz_catch are macros around setjmp/longjmp, NOT C++ exceptions.
    // Any fz_* call that fails will longjmp to fz_catch. This is why MuPDF
    // resources use fz_drop_* (manual cleanup) rather than RAII.
    fz_try(ctx) {
        // Wrap our raw bytes in a MuPDF stream. fz_open_memory does NOT take
        // ownership — the caller must keep `data` alive while the stream exists.
        stream = fz_open_memory(ctx, data, len);

        // ".pdf" is the "magic" — MuPDF uses it for format detection.
        // Could also pass the filename or MIME type.
        doc = fz_open_document_with_stream(ctx, ".pdf", stream);

        int page_count = fz_count_pages(ctx, doc);

        // fz_buffer is a growable byte array. fz_output writes into it.
        // This is how MuPDF does "write to string" — buffer + output adapter.
        buf = fz_new_buffer(ctx, 4096);
        out = fz_new_output_with_buffer(ctx, buf);

        for (int i = 0; i < page_count; i++) {
            fz_page* page = fz_load_page(ctx, doc, i);

            // Structured text extraction pipeline:
            // 1. Create an empty stext_page (will hold text blocks/lines/chars)
            // 2. Create a device that populates the stext_page
            // 3. Run the PDF page through the device
            // 4. Print the stext_page as plain text
            //
            // "Device" is MuPDF's rendering abstraction — a display list
            // interpreter. Different devices: draw to image, extract text,
            // output SVG, etc. Same page, different device = different output.
            fz_stext_page* stext = fz_new_stext_page(ctx, fz_bound_page(ctx, page));
            fz_device* dev = fz_new_stext_device(ctx, stext, nullptr);
            fz_run_page(ctx, page, dev, fz_identity, nullptr);
            fz_close_device(ctx, dev);
            fz_drop_device(ctx, dev);

            // fz_print_stext_page_as_text walks block→line→char and writes
            // Unicode text. It handles reading order, line breaks, and
            // paragraph detection. Much better than manual char iteration
            // for our use case (feeding text to an LLM).
            fz_print_stext_page_as_text(ctx, out, stext);

            fz_drop_stext_page(ctx, stext);
            fz_drop_page(ctx, page);
        }

        // fz_close_output flushes. fz_string_from_buffer gives a
        // null-terminated C string owned by the buffer — must copy
        // before dropping the buffer.
        fz_close_output(ctx, out);
        result = std::string(fz_string_from_buffer(ctx, buf));
    }
    fz_catch(ctx) {
        std::cout << "[pdf] PDF extraction failed: "
                  << fz_caught_message(ctx) << std::endl;
    }

    // fz_drop_* is safe on nullptr (no-op), so we don't need null checks.
    // Order doesn't strictly matter for independent resources, but
    // dropping in reverse-creation order is conventional.
    fz_drop_output(ctx, out);
    fz_drop_buffer(ctx, buf);
    fz_drop_document(ctx, doc);
    fz_drop_stream(ctx, stream);
    fz_drop_context(ctx);

    return result;
}
