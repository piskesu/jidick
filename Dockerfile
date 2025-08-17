FROM golang:1.23.0-alpine AS base
RUN sed -i 's/dl-cdn.alpinelinux.org/mirrors.aliyun.com/g' /etc/apk/repositories
RUN apk add --no-cache \
        make \
        clang15 \
        libbpf-dev \
        bpftool \
        curl \
        git

ENV PATH=$PATH:/usr/lib/llvm15/bin

# build huatuo components
FROM base AS build
ARG BUILD_PATH=${BUILD_PATH:-/go/huatuo-bamai}
ARG RUN_PATH=${RUN_PATH:-/home/huatuo-bamai}
WORKDIR ${BUILD_PATH}
COPY . .
RUN make && \
    mkdir -p ${RUN_PATH}/bpf && \
    cp ${BUILD_PATH}/_output/bin/* ${RUN_PATH}/ && \
    cp ${BUILD_PATH}/huatuo-bamai.conf ${RUN_PATH}/huatuo-bamai.conf && \
    cp ${BUILD_PATH}/bpf/*.o ${RUN_PATH}/bpf/

# Comment following line if elasticsearch is needed and repalce the ES configs in huatuo-bamai.conf
RUN sed -i 's/"http:\/\/127.0.0.1:9200"/""/' ${RUN_PATH}/huatuo-bamai.conf

# final public image
FROM alpine:3.22.0 AS run
ARG RUN_PATH=${RUN_PATH:-/home/huatuo-bamai}
RUN apk add --no-cache curl
COPY --from=build ${RUN_PATH} ${RUN_PATH}
WORKDIR ${RUN_PATH}
CMD ["./huatuo-bamai", "--region", "example", "--config", "huatuo-bamai.conf"]
