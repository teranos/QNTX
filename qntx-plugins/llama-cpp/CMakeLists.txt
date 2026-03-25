cmake_minimum_required(VERSION 3.20)
project(qntx-llama-cpp VERSION 0.1.0 LANGUAGES CXX C)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# llama.cpp — fetched at build time if not already vendored.
# Same commit pinned in flake.nix for reproducibility.
include(FetchContent)
if(EXISTS "${CMAKE_SOURCE_DIR}/vendor/llama.cpp/CMakeLists.txt")
    set(LLAMA_SOURCE_DIR "${CMAKE_SOURCE_DIR}/vendor/llama.cpp")
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

if(APPLE)
    set(GGML_METAL ON CACHE BOOL "Enable Metal acceleration" FORCE)
else()
    set(GGML_METAL OFF CACHE BOOL "Metal is macOS-only" FORCE)
endif()
set(LLAMA_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_TESTS OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_SERVER OFF CACHE BOOL "" FORCE)
add_subdirectory(${LLAMA_SOURCE_DIR} llama.cpp)

# MuPDF — system/nix library for PDF text extraction.
# Install via: brew install mupdf-tools (macOS) or nix with flake.
find_library(MUPDF_LIB mupdf REQUIRED)
find_library(MUPDF_THIRD_LIB mupdf-third)
find_path(MUPDF_INCLUDE_DIR mupdf/fitz.h REQUIRED)

# gRPC + Protobuf
find_package(protobuf CONFIG REQUIRED)
find_package(gRPC CONFIG REQUIRED)

# Proto generation — PROTO_DIR can be overridden for sandboxed builds (Nix)
if(NOT DEFINED PROTO_DIR)
    set(PROTO_DIR "${CMAKE_SOURCE_DIR}/../../plugin/grpc/protocol")
endif()
set(PROTO_GEN_DIR "${CMAKE_BINARY_DIR}/generated")
file(MAKE_DIRECTORY ${PROTO_GEN_DIR})

function(generate_proto PROTO_FILE)
    get_filename_component(PROTO_NAME ${PROTO_FILE} NAME_WE)
    set(PROTO_SRC "${PROTO_GEN_DIR}/${PROTO_NAME}.pb.cc")
    set(PROTO_HDR "${PROTO_GEN_DIR}/${PROTO_NAME}.pb.h")
    set(GRPC_SRC "${PROTO_GEN_DIR}/${PROTO_NAME}.grpc.pb.cc")
    set(GRPC_HDR "${PROTO_GEN_DIR}/${PROTO_NAME}.grpc.pb.h")

    add_custom_command(
        OUTPUT ${PROTO_SRC} ${PROTO_HDR} ${GRPC_SRC} ${GRPC_HDR}
        COMMAND protobuf::protoc
            --proto_path=${PROTO_DIR}
            --cpp_out=${PROTO_GEN_DIR}
            --grpc_out=${PROTO_GEN_DIR}
            --plugin=protoc-gen-grpc=$<TARGET_FILE:gRPC::grpc_cpp_plugin>
            ${PROTO_DIR}/${PROTO_FILE}
        DEPENDS ${PROTO_DIR}/${PROTO_FILE}
        COMMENT "Generating C++ protos for ${PROTO_FILE}"
    )

    set(GENERATED_SRCS ${GENERATED_SRCS} ${PROTO_SRC} ${GRPC_SRC} PARENT_SCOPE)
endfunction()

set(GENERATED_SRCS "")
generate_proto(domain.proto)
generate_proto(llm.proto)

# Plugin binary
add_executable(qntx-llama-cpp-plugin
    src/main.cpp
    src/plugin.cpp
    src/inference.cpp
    src/vocab_projection.cpp
    src/pdf_extract.cpp
    ${GENERATED_SRCS}
)

target_include_directories(qntx-llama-cpp-plugin PRIVATE
    ${PROTO_GEN_DIR}
    ${LLAMA_SOURCE_DIR}/include
    ${LLAMA_SOURCE_DIR}/src
    ${LLAMA_SOURCE_DIR}/ggml/include
    ${MUPDF_INCLUDE_DIR}
)

target_link_libraries(qntx-llama-cpp-plugin PRIVATE
    llama
    gRPC::grpc++
    protobuf::libprotobuf
    ${MUPDF_LIB}
)

if(APPLE)
    target_link_libraries(qntx-llama-cpp-plugin PRIVATE "-framework Accelerate")
    target_compile_definitions(qntx-llama-cpp-plugin PRIVATE ACCELERATE_NEW_LAPACK)
endif()

# mupdf-third bundles freetype/zlib/etc — only present with static MuPDF installs
if(MUPDF_THIRD_LIB)
    target_link_libraries(qntx-llama-cpp-plugin PRIVATE ${MUPDF_THIRD_LIB})
endif()

install(TARGETS qntx-llama-cpp-plugin DESTINATION bin)
