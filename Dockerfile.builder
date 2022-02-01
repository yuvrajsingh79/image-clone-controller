FROM golang:1.16.10

WORKDIR /go/src/github.com/yuvrajsingh79/image-clone-controller
ADD . /go/src/github.com/yuvrajsingh79/image-clone-controller

ARG TAG
ARG OS
ARG ARCH

CMD ["./build-bin.sh"]