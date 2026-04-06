#pragma once

#include <cstddef>
#include <cstdint>
#include <string>

// Extract all text from a PDF buffer using MuPDF.
// Returns empty string on failure.
std::string extract_pdf_text(const uint8_t* data, size_t len);
