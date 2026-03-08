#!/bin/sh
# Generate CA + leaf cert for ix-net HTTPS proxy.
#
# The CA cert is what Claude Code must trust (NODE_EXTRA_CA_CERTS).
# The leaf cert is for api.anthropic.com — presented to clients during MITM.
#
# Output:
#   ca.key          — CA private key
#   ca.pem          — CA certificate (give to NODE_EXTRA_CA_CERTS)
#   leaf.key        — Leaf private key (for api.anthropic.com)
#   leaf.pem        — Leaf certificate (signed by CA)
#
# Usage:
#   cd qntx-plugins/ix-net/certs && sh generate.sh

set -e

DIR="$(cd "$(dirname "$0")" && pwd)"
cd "$DIR"

echo "==> Generating CA key + cert..."
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:prime256v1 -out ca.key
openssl req -new -x509 -key ca.key -sha256 -days 3650 \
    -subj "/CN=ix-net QNTX Proxy CA" \
    -out ca.pem

echo "==> Generating leaf key + CSR for api.anthropic.com..."
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:prime256v1 -out leaf.key
openssl req -new -key leaf.key -sha256 \
    -subj "/CN=api.anthropic.com" \
    -out leaf.csr

# SAN extension config
cat > leaf_ext.cnf <<EOF
[v3_ext]
subjectAltName = DNS:api.anthropic.com, DNS:*.anthropic.com
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
EOF

echo "==> Signing leaf cert with CA..."
openssl x509 -req -in leaf.csr -CA ca.pem -CAkey ca.key \
    -CAcreateserial -sha256 -days 825 \
    -extfile leaf_ext.cnf -extensions v3_ext \
    -out leaf.pem

# Clean up intermediates
rm -f leaf.csr leaf_ext.cnf ca.srl

echo ""
echo "Done. Files:"
echo "  CA cert:   $DIR/ca.pem   (use with NODE_EXTRA_CA_CERTS)"
echo "  CA key:    $DIR/ca.key"
echo "  Leaf cert: $DIR/leaf.pem (api.anthropic.com)"
echo "  Leaf key:  $DIR/leaf.key"
echo ""
echo "Usage:"
echo "  export NODE_EXTRA_CA_CERTS=$DIR/ca.pem"
echo "  export HTTPS_PROXY=http://localhost:9100"
