#!/bin/bash

set -e

go build -C ./gen
mv ./gen/gen $GOPATH/bin/protoc-gen-terraform

rm -fr build || true
mkdir build || true

pushd build

cp -R $(go list -m -f '{{.Dir}}' gitlab.authwise.io/authwise/api-client-go)/proto/types-core/* .
find . -type f | xargs chmod 644
find . -type d | xargs chmod 755
# cp -R ../protoinject/* .

cat << EOF > buf.gen.yaml
version: v1
managed:
  enabled: true
plugins:
  - plugin: terraform
    out: ../internal/provider
    opt:
      - paths=source_relative
EOF


buf generate

popd
rm -fr build || true
