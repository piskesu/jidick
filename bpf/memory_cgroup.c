#include "bpf_common.h"
#include "vmlinux.h"
#include "vmlinux_sched.h"

#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

char __license[] SEC("license") = "Dual MIT/GPL";

struct mem_cgroup_metric {
	/* cg: direct reclaim count caused by try_charge */
	unsigned long directstall_count;
};

struct {
	__uint(type, BPF_MAP_TYPE_HASH);
	__type(key, unsigned long);
	__type(value, struct mem_cgroup_metric);
	__uint(max_entries, 10240);
} mem_cgroup_map SEC(".maps");

SEC("tracepoint/vmscan/mm_vmscan_memcg_reclaim_begin")
int tracepoint_vmscan_mm_vmscan_memcg_reclaim_begin(struct pt_regs *ctx)
{
	struct cgroup_subsys_state *mm_subsys;
	struct mem_cgroup_metric *valp;
	struct task_struct *task;

	task = (struct task_struct *)bpf_get_current_task();
	if (BPF_CORE_READ(task, flags) & PF_KSWAPD)
		return 0;

	mm_subsys = BPF_CORE_READ(task, cgroups, subsys[memory_cgrp_id]);
	valp	  = bpf_map_lookup_elem(&mem_cgroup_map, &mm_subsys);
	if (!valp) {
		struct mem_cgroup_metric new_metrics = {
			.directstall_count = 1,
		};
		bpf_map_update_elem(&mem_cgroup_map, &mm_subsys, &new_metrics,
				    BPF_ANY);
		return 0;
	}

	__sync_fetch_and_add(&valp->directstall_count, 1);
	return 0;
}

SEC("kprobe/mem_cgroup_css_released")
int kprobe_mem_cgroup_css_released(struct pt_regs *ctx)
{
	u64 css = PT_REGS_PARM1(ctx);
	bpf_map_delete_elem(&mem_cgroup_map, &css);
	return 0;
}
