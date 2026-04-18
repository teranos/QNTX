# Reads INPUT file and writes OUTPUT as a C raw string literal.
# Usage: cmake -DINPUT=file.metal -DOUTPUT=file.metal.inc -P embed_string.cmake
file(READ "${INPUT}" CONTENT)
# Escape backslashes and quotes for C string
string(REPLACE "\\" "\\\\" CONTENT "${CONTENT}")
string(REPLACE "\"" "\\\"" CONTENT "${CONTENT}")
# Convert newlines to \n" + newline + "
string(REPLACE "\n" "\\n\"\n\"" CONTENT "${CONTENT}")
file(WRITE "${OUTPUT}" "\"${CONTENT}\"")
