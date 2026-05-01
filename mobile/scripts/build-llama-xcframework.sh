#!/usr/bin/env bash
# Build llama.cpp + ggml as a multi-arch XCFramework for iOS device, iOS
# simulator, and macOS. Output: mobile/Frameworks/llama.xcframework
#
# Run on a Mac with Xcode + the iOS SDK installed.
#
# llama.cpp ships its own Apple toolchain support — we drive it via cmake
# with the Xcode generator and the iOS toolchain hints, then bundle the
# resulting frameworks into one XCFramework.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
MOBILE_DIR="$(cd "${SCRIPT_DIR}/.." && pwd)"
REPO_ROOT="$(cd "${MOBILE_DIR}/.." && pwd)"

LLAMA_SRC="${REPO_ROOT}/qntx-plugins/scry/vendor/llama.cpp"
if [ ! -d "${LLAMA_SRC}" ]; then
    echo "llama.cpp not found at ${LLAMA_SRC}"
    echo "checkout: cd qntx-plugins/scry && cmake -S . -B build  # fetches llama.cpp"
    echo "or vendor manually: git clone https://github.com/ggml-org/llama.cpp ${LLAMA_SRC}"
    exit 1
fi

BUILD_DIR="${MOBILE_DIR}/build"
FRAMEWORKS_DIR="${MOBILE_DIR}/Frameworks"
mkdir -p "${BUILD_DIR}" "${FRAMEWORKS_DIR}"

build_for() {
    local platform="$1"
    local arch="$2"
    local sdk="$3"
    local out="${BUILD_DIR}/${platform}-${arch}"

    rm -rf "${out}"
    cmake -S "${LLAMA_SRC}" -B "${out}" \
        -G Xcode \
        -DCMAKE_SYSTEM_NAME=iOS \
        -DCMAKE_OSX_SYSROOT="${sdk}" \
        -DCMAKE_OSX_ARCHITECTURES="${arch}" \
        -DCMAKE_OSX_DEPLOYMENT_TARGET=17.0 \
        -DBUILD_SHARED_LIBS=NO \
        -DLLAMA_BUILD_EXAMPLES=OFF \
        -DLLAMA_BUILD_TESTS=OFF \
        -DLLAMA_BUILD_SERVER=OFF \
        -DLLAMA_BUILD_TOOLS=OFF \
        -DGGML_METAL=ON \
        -DGGML_METAL_EMBED_LIBRARY=ON \
        -DGGML_OPENMP=OFF \
        -DGGML_ACCELERATE=ON

    cmake --build "${out}" --config Release -- -quiet
}

build_for iphoneos arm64 iphoneos
build_for iphonesimulator arm64 iphonesimulator
# Optionally also macOS arm64 for Mac Catalyst / dev runs.
# build_for macosx arm64 macosx

# llama.cpp produces multiple static libs (libllama.a, libggml.a, etc.)
# Combine each platform's libs into a single static lib, then xcframework.
combine_libs() {
    local platform="$1"
    local out="${BUILD_DIR}/${platform}-arm64/Release-${platform}"
    local combined="${BUILD_DIR}/libllama-combined-${platform}.a"
    libtool -static -o "${combined}" \
        "${out}/libllama.a" \
        "${out}/libggml.a" \
        "${out}/libggml-base.a" \
        "${out}/libggml-cpu.a" \
        "${out}/libggml-metal.a" \
        "${out}/libggml-blas.a" 2>/dev/null || \
    libtool -static -o "${combined}" "${out}"/lib*.a
    echo "${combined}"
}

DEVICE_LIB="$(combine_libs iphoneos)"
SIM_LIB="$(combine_libs iphonesimulator)"

HEADERS_DIR="${BUILD_DIR}/headers"
rm -rf "${HEADERS_DIR}"
mkdir -p "${HEADERS_DIR}"
cp "${LLAMA_SRC}"/include/*.h "${HEADERS_DIR}/" 2>/dev/null || true
cp "${LLAMA_SRC}"/ggml/include/*.h "${HEADERS_DIR}/" 2>/dev/null || true

XCF="${FRAMEWORKS_DIR}/llama.xcframework"
rm -rf "${XCF}"
xcodebuild -create-xcframework \
    -library "${DEVICE_LIB}" -headers "${HEADERS_DIR}" \
    -library "${SIM_LIB}"    -headers "${HEADERS_DIR}" \
    -output "${XCF}"

echo
echo "wrote ${XCF}"
