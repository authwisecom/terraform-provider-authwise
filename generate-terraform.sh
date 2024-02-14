#!/bin/bash

set +e

go install gitlab.authwise.io/authwise/protoc-gen-terraform@40f306f2bf1d1a4521f7188615bc8b92317f03e3

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
