#!/bin/sh

ELASTICSEARCH_HOST=${ELASTICSEARCH_HOST:-localhost}
ELASTIC_PASSWORD=${ELASTIC_PASSWORD:-huatuo-bamai}

BUILD_PATH=${BUILD_PATH:-/go/huatuo-bamai}
RUN_PATH=${RUN_PATH:-/home/huatuo-bamai}

# Wait for Elasticsearch to be ready
wait_for_elasticsearch() {
    args="-s -D- -m15 -w '%{http_code}' http://${ELASTICSEARCH_HOST}:9200/"
    if [ -n "${ELASTIC_PASSWORD}" ]; then
        args="$args -u elastic:${ELASTIC_PASSWORD}"
    fi

    result=1
    output=""

    # retry for up to 180 seconds
    for sec in $(seq 1 180); do
        exit_code=0
        output=$(eval "curl $args") || exit_code=$?
        # echo "exec curl $args, exit code: $exit_code, output: $output"
        if [ $exit_code -ne 0 ]; then
            result=$exit_code
        fi

        # Extract the last three characters of the output to check the HTTP status code
        http_code=$(echo "$output" | tail -c 4)
        if [ "$http_code" -eq 200 ]; then
            result=0
            break
        fi

        echo "Waiting for Elasticsearch ready... ${sec}s"
        sleep 1
    done

    if [ $result -ne 0 ] && [ "$http_code" -ne 000 ]; then
        echo "$output" | head -c -3
    fi

    if [ $result -ne 0 ]; then
        case $result in
            6)
                echo 'Could not resolve host. Is Elasticsearch running?'
                ;;
            7)
                echo 'Failed to connect to host. Is Elasticsearch healthy?'
                ;;
            28)
                echo 'Timeout connecting to host. Is Elasticsearch healthy?'
                ;;
            *)
                echo "Connection to Elasticsearch failed. Exit code: ${result}"
                ;;
        esac

        exit $result
    fi
}

# Compile and copy huatuo-bamai, .conf, bpf.o, cmd-tools to run path
prepare_run_env() {
    # compile huatuo-bamai
    if [ ! -x "$BUILD_PATH/_output/bin/huatuo-bamai" ]; then
        cd $BUILD_PATH && make clean
        bpftool btf dump file /sys/kernel/btf/vmlinux format c > bpf/include/vmlinux.h || {
            echo "Failed to dump vmlinux.h"
            exit 1
        }
        make || {
            echo "Failed to compile huatuo-bamai"
            exit 1
        }
    fi
    # copy huatuo-bamai, .conf, bpf.o, cmd-tools to run path
    cp $BUILD_PATH/_output/bin/huatuo-bamai $RUN_PATH/huatuo-bamai || {
        echo "Failed to copy huatuo-bamai"
        exit 1
    }
    cp $BUILD_PATH/huatuo-bamai.conf $RUN_PATH/huatuo-bamai.conf || {
        echo "Failed to copy huatuo-bamai.conf"
        exit 1
    }
    mkdir -p $RUN_PATH/bpf && cp $BUILD_PATH/bpf/*.o $RUN_PATH/bpf/ || {
        echo "Failed to copy bpf files"
        exit 1
    }
    mkdir -p $RUN_PATH/tracer && find $BUILD_PATH/cmd/ -type f -name "*.bin" -exec cp {} $RUN_PATH/tracer/ \; || {
        echo "Failed to copy cmd-tools files"
        exit 1
    }
}

# Prepare run env for huatuo-bamai
prepare_run_env
echo "huatuo-bamai run env is ready."

wait_for_elasticsearch
sleep 5 # Waiting for initialization of Elasticsearch built-in users
echo "Elasticsearch is ready."

# Run huatuo-bamai
cd $RUN_PATH
exec ./huatuo-bamai --region example --config huatuo-bamai.conf
