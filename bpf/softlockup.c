#include "vmlinux.h"

#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

#include "bpf_common.h"
#include "bpf_ratelimit.h"

char __license[] SEC("license") = "Dual MIT/GPL";

#define CPU_NUM 128
BPF_RATELIMIT_IN_MAP(rate, 1, CPU_NUM * 10000, 0);

struct {
	__uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
	__uint(key_size, sizeof(int));
	__uint(value_size, sizeof(u32));
} softlockup_perf_events SEC(".maps");

struct softlockup_info {
	u32 cpu;
	u32 pid;
	char comm[COMPAT_TASK_COMM_LEN];
};

SEC("kprobe/watchdog_timer_fn+442")
int kprobe_watchdog_timer_fn(struct pt_regs *ctx)
{
	struct softlockup_info info = {};
	struct task_struct *task;

	if (bpf_ratelimited_in_map(ctx, rate))
		return 0;

	info.cpu = bpf_get_smp_processor_id();
	task	 = (struct task_struct *)bpf_get_current_task();
	info.pid = bpf_get_current_pid_tgid() & 0xffffffffUL;
	BPF_CORE_READ_STR_INTO(&info.comm, task, comm);
	bpf_perf_event_output(ctx, &softlockup_perf_events,
			      COMPAT_BPF_F_CURRENT_CPU, &info, sizeof(info));
	return 0;
}
