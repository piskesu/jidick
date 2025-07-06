#!/bin/sh

ELASTICSEARCH_HOST=${ELASTICSEARCH_HOST:-localhost}
ELASTIC_PASSWORD=${ELASTIC_PASSWORD:-huatuo-bamai}

# Wait for Elasticsearch to be ready
# ref: https://github.com/deviantony/docker-elk/blob/main/setup/entrypoint.sh
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

    return $result
}

exit_code=0
wait_for_elasticsearch || exit_code=$?
if [ $exit_code -ne 0 ]; then
    case $exit_code in
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
            echo "Connection to Elasticsearch failed. Exit code: ${exit_code}"
            ;;
    esac

    exit $exit_code
fi

# Waiting for initialization of Elasticsearch built-in users
sleep 5

echo "Elasticsearch is ready."

# Run huatuo-bamai
exec _output/bin/huatuo-bamai --region example --config huatuo-bamai.conf