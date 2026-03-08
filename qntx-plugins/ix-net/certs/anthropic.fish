#!/usr/bin/env fish
# Generate CA + leaf cert for api.anthropic.com MITM.
#
# The CA cert is what Claude Code must trust (NODE_EXTRA_CA_CERTS).
# The leaf cert is presented to clients during TLS interception.
#
# Output:
#   ca.key          — CA private key
#   ca.pem          — CA certificate (give to NODE_EXTRA_CA_CERTS)
#   leaf.key        — Leaf private key (for api.anthropic.com)
#   leaf.pem        — Leaf certificate (signed by CA)
#
# Usage:
#   cd qntx-plugins/ix-net/certs && fish anthropic.fish

set DIR (cd (dirname (status filename)); and pwd)
cd $DIR

echo "==> Generating CA key + cert..."
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:prime256v1 -out ca.key
or exit 1
openssl req -new -x509 -key ca.key -sha256 -days 3650 \
    -subj "/CN=ix-net QNTX Proxy CA" \
    -out ca.pem
or exit 1

echo "==> Generating leaf key + CSR for api.anthropic.com..."
openssl genpkey -algorithm EC -pkeyopt ec_paramgen_curve:prime256v1 -out leaf.key
or exit 1
openssl req -new -key leaf.key -sha256 \
    -subj "/CN=api.anthropic.com" \
    -out leaf.csr
or exit 1

# SAN extension config
printf '[v3_ext]
subjectAltName = DNS:api.anthropic.com, DNS:*.anthropic.com
basicConstraints = CA:FALSE
keyUsage = digitalSignature, keyEncipherment
extendedKeyUsage = serverAuth
' > leaf_ext.cnf

echo "==> Signing leaf cert with CA..."
openssl x509 -req -in leaf.csr -CA ca.pem -CAkey ca.key \
    -CAcreateserial -sha256 -days 825 \
    -extfile leaf_ext.cnf -extensions v3_ext \
    -out leaf.pem
or exit 1

rm -f leaf.csr leaf_ext.cnf ca.srl

echo ""
echo "Done. Files:"
echo "  CA cert:   $DIR/ca.pem   (use with NODE_EXTRA_CA_CERTS)"
echo "  CA key:    $DIR/ca.key"
echo "  Leaf cert: $DIR/leaf.pem (api.anthropic.com)"
echo "  Leaf key:  $DIR/leaf.key"
