#include "vmlinux.h"
#include "bpf_common.h"
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_core_read.h>
#include "bpf_ratelimit.h"

char __license[] SEC("license") = "Dual MIT/GPL";

#define CPU_NUM 128
BPF_RATELIMIT_IN_MAP(rate, 1, CPU_NUM * 10000, 0);


struct {
    __uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
    __uint(key_size, sizeof(int));
    __uint(value_size, sizeof(u32));
} hungtask_perf_events SEC(".maps");

struct hungtask_info {
    int32_t pid;
    char comm[TASK_COMM_LEN];
};

struct tracepoint_args {
	unsigned long pad;
	char comm[TASK_COMM_LEN];
	int pid;
};

SEC("tracepoint/sched/sched_process_hang")
int tracepoint_sched_process_hang(struct tracepoint_args *ctx)
{
    struct hungtask_info info = {};

    if (bpf_ratelimited_in_map(ctx, rate))
        return 0;
    info.pid = ctx->pid;
	// custom defined struct can't use BPF_CORE_READ_STR_INTO()
	bpf_probe_read_str(&info.comm, TASK_COMM_LEN, ctx->comm);
    bpf_perf_event_output(ctx, &hungtask_perf_events, BPF_F_CURRENT_CPU, &info, sizeof(info));
    return 0;
}
