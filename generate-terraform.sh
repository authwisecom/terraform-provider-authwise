#!/bin/bash

set +e

go install gitlab.authwise.io/authwise/protoc-gen-terraform@e8ffb2cdb1b60ff6258224f3f6f3b499b150b3a4

rm -fr build || true
mkdir build || true
pushd build

cp -R $(go list -m -f '{{.Dir}}' gitlab.authwise.io/authwise/api-client-go)/proto/types-core/* .
find . -type f | xargs chmod 644
find . -type d | xargs chmod 755

cat << EOF > buf.gen.yaml
version: v1
managed:
  enabled: true
plugins:
  - plugin: terraform
    out: ../internal/model
    opt:
      - paths=source_relative
EOF

buf generate
popd
rm -fr build || true
