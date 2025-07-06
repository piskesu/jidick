ARG BUILD_PATH=/go/huatuo-bamai
ARG RUN_PATH=/home/huatuo-bamai

# https://hub.docker.com/_/golang/tags?name=1.22.4
FROM golang:1.22.4-alpine AS base

# Install dependencies for build
RUN apk add --no-cache \
        make \
        clang15 \
        libbpf-dev \
        bpftool \
        curl && \
        bpftool btf dump file /sys/kernel/btf/vmlinux format c > bpf/include/vmlinux.h
ENV PATH=$PATH:/usr/lib/llvm15/bin

# Build huatuo
FROM base AS build
ARG BUILD_PATH
WORKDIR ${BUILD_PATH}
COPY . .
RUN make

# Release huatuo image
FROM base AS run
ARG BUILD_PATH
ARG RUN_PATH
WORKDIR ${RUN_PATH}
COPY --from=build \
        ${BUILD_PATH}/_output ./_output
COPY --from=build \
        ${BUILD_PATH}/huatuo-bamai.conf .

CMD ["/run.sh"]