cmake_minimum_required(VERSION 3.20)
project(qntx-llama-cpp VERSION 0.1.0 LANGUAGES CXX C)

set(CMAKE_CXX_STANDARD 17)
set(CMAKE_CXX_STANDARD_REQUIRED ON)

# llama.cpp — built as a library
set(GGML_METAL ON CACHE BOOL "Enable Metal acceleration" FORCE)
set(LLAMA_BUILD_EXAMPLES OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_TESTS OFF CACHE BOOL "" FORCE)
set(LLAMA_BUILD_SERVER OFF CACHE BOOL "" FORCE)
add_subdirectory(vendor/llama.cpp)

# gRPC + Protobuf
find_package(protobuf CONFIG REQUIRED)
find_package(gRPC CONFIG REQUIRED)

# Proto generation
set(PROTO_DIR "${CMAKE_SOURCE_DIR}/../../plugin/grpc/protocol")
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
    ${GENERATED_SRCS}
)

target_include_directories(qntx-llama-cpp-plugin PRIVATE
    ${PROTO_GEN_DIR}
    vendor/llama.cpp/include
)

target_link_libraries(qntx-llama-cpp-plugin PRIVATE
    llama
    gRPC::grpc++
    protobuf::libprotobuf
)

install(TARGETS qntx-llama-cpp-plugin DESTINATION bin)
