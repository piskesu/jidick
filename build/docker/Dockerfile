# https://hub.docker.com/_/golang/tags?name=1.22.4
FROM golang:1.22.4-alpine AS base
ARG RUN_PATH=${RUN_PATH:-/home/huatuo-bamai}

# Install dependencies for build
RUN apk add --no-cache \
        make \
        clang15 \
        libbpf-dev \
        bpftool \
        curl
ENV PATH=$PATH:/usr/lib/llvm15/bin
WORKDIR ${RUN_PATH}
CMD ["./run.sh"]
