# Shared C++ proto generation for all plugins.
# Usage:
#   set(PROTO_DIR "${CMAKE_SOURCE_DIR}/../../plugin/grpc/protocol")  # or -DPROTO_DIR=
#   include(${PROTO_DIR}/proto.cmake)
#   generate_proto(domain.proto)
#   generate_proto(llm.proto)
#   # then add ${GENERATED_SRCS} to your target and ${PROTO_GEN_DIR} to includes
#
# NOTE: Default PROTO_DIR assumes the plugin lives 2 levels below the qntx repo
# root (e.g. qntx-plugins/gaze/, ctp/werf/). Override PROTO_DIR if this changes.

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

    # PROTO_DIR for bare imports, PROTO_DIR/../../.. for qualified imports
    # (e.g. ground.proto imports "plugin/grpc/protocol/atsstore.proto")
    get_filename_component(PROTO_ROOT "${PROTO_DIR}/../../.." ABSOLUTE)
    add_custom_command(
        OUTPUT ${PROTO_SRC} ${PROTO_HDR} ${GRPC_SRC} ${GRPC_HDR}
        COMMAND protobuf::protoc
            --proto_path=${PROTO_DIR}
            --proto_path=${PROTO_ROOT}
            --cpp_out=${PROTO_GEN_DIR}
            --grpc_out=${PROTO_GEN_DIR}
            --plugin=protoc-gen-grpc=$<TARGET_FILE:gRPC::grpc_cpp_plugin>
            ${PROTO_DIR}/${PROTO_FILE}
        DEPENDS ${PROTO_DIR}/${PROTO_FILE}
        COMMENT "Generating C++ protos for ${PROTO_FILE}"
    )

    set(GENERATED_SRCS ${GENERATED_SRCS} ${PROTO_SRC} ${GRPC_SRC} PARENT_SCOPE)
endfunction()
