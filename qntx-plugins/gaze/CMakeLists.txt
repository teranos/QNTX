cmake_minimum_required(VERSION 3.20)
project(qntx-gaze VERSION 0.1.0 LANGUAGES CXX C)

set(CMAKE_EXPORT_COMPILE_COMMANDS ON)
set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# llama.cpp — fetched at build time if not already vendored.
# Same commit pinned in flake.nix for reproducibility.
#
# Options must be set BEFORE FetchContent_MakeAvailable / add_subdirectory
# so llama.cpp's CMakeLists.txt sees them.
if(APPLE)
    set(GGML_METAL ON CACHE BOOL "Enable Metal acceleration" FORCE)
else()
    set(GGML_METAL OFF CACHE BOOL "Metal is macOS-only" FORCE)
endif()
set(LLAMA_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_TESTS OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_SERVER OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_COMMON ON CACHE BOOL "Build common lib (needed for mtmd)" FORCE)
set(LLAMA_BUILD_TOOLS ON CACHE BOOL "Build tools including mtmd" FORCE)

include(FetchContent)

if(EXISTS "${CMAKE_SOURCE_DIR}/vendor/llama.cpp/CMakeLists.txt")
    set(LLAMA_SOURCE_DIR "${CMAKE_SOURCE_DIR}/vendor/llama.cpp")
    add_subdirectory(${LLAMA_SOURCE_DIR} llama.cpp)
else()
    FetchContent_Declare(
        llama_cpp
        GIT_REPOSITORY https://github.com/ggml-org/llama.cpp.git
        GIT_TAG        db9d8aa428012cc5593e18635d4c3c54095f5138
        GIT_SHALLOW    TRUE
    )
    FetchContent_MakeAvailable(llama_cpp)
    set(LLAMA_SOURCE_DIR "${llama_cpp_SOURCE_DIR}")
endif()

# MuPDF — system/nix library for PDF text extraction.
# Install via: brew install mupdf-tools (macOS) or nix with flake.
find_library(MUPDF_LIB mupdf REQUIRED)
find_library(MUPDF_THIRD_LIB mupdf-third)
find_path(MUPDF_INCLUDE_DIR mupdf/fitz.h REQUIRED)

# gRPC + Protobuf
find_package(protobuf CONFIG REQUIRED)
find_package(gRPC CONFIG REQUIRED)

# Proto generation — shared function from proto.cmake
set(GENERATED_SRCS "")
include(${CMAKE_SOURCE_DIR}/../../plugin/grpc/protocol/proto.cmake)
generate_proto(domain.proto)
generate_proto(llm.proto)

# Plugin binary — no Metal renderer, no PCA, no ATS client
add_executable(qntx-gaze-plugin
    src/main.cpp
    src/plugin.cpp
    src/inference.cpp
    src/vision.cpp
    src/llm_service.cpp
    src/pdf_extract.cpp
    ${GENERATED_SRCS}
)

target_include_directories(qntx-gaze-plugin PRIVATE
    ${PROTO_GEN_DIR}
    ${LLAMA_SOURCE_DIR}/include
    ${LLAMA_SOURCE_DIR}/src
    ${LLAMA_SOURCE_DIR}/ggml/include
    ${LLAMA_SOURCE_DIR}/common
    ${LLAMA_SOURCE_DIR}/tools/mtmd
    ${MUPDF_INCLUDE_DIR}
)

target_link_libraries(qntx-gaze-plugin PRIVATE
    llama
    mtmd
    gRPC::grpc++
    protobuf::libprotobuf
    ${MUPDF_LIB}
)

# mupdf-third bundles freetype/zlib/etc — only present with static MuPDF installs
if(MUPDF_THIRD_LIB)
    target_link_libraries(qntx-gaze-plugin PRIVATE ${MUPDF_THIRD_LIB})
endif()

install(TARGETS qntx-gaze-plugin DESTINATION bin)
