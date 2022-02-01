#!/bin/bash
cd /go/src/github.com/yuvrajsingh79/image-clone-controller
CGO_ENABLED=0 go build -a -ldflags '-X main.vendorVersion='"image-clone-controller-${TAG}"' -extldflags "-static"' -o /go/bin/image-clone-controller ./cmd/