#include "vmlinux.h"

#include <bpf/bpf_core_read.h>
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>

#include "bpf_common.h"

#define CGROUP_KNODE_NAME_MAXLEN 64

struct cgroup_perf_event_t {
	u64 cgroup;
	u64 ops_type;
	s32 cgroup_root;
	s32 cgroup_level;
	u64 css[CGROUP_SUBSYS_COUNT];
	char knode_name[CGROUP_KNODE_NAME_MAXLEN + 2];
};

struct {
	__uint(type, BPF_MAP_TYPE_PERF_EVENT_ARRAY);
	__uint(key_size, sizeof(int));
	__uint(value_size, sizeof(u32));
} cgroup_perf_events SEC(".maps");

char __license[] SEC("license") = "GPL";

SEC("kprobe/cgroup_clone_children_read")
int bpf_cgroup_clone_children_read_prog(struct pt_regs *ctx)
{
	struct cgroup_subsys_state *css = (void *)PT_REGS_PARM1(ctx);
	struct cgroup *cgrp		= BPF_CORE_READ(css, cgroup);
	struct cgroup_perf_event_t data = {};
	int knode_len;

	/* knode name */
	knode_len =
	    bpf_probe_read_str(&data.knode_name, sizeof(data.knode_name),
			       BPF_CORE_READ(cgrp, kn, name));
	if (knode_len != CGROUP_KNODE_NAME_MAXLEN + 1)
		return 0;

	data.cgroup	  = (u64)cgrp;
	data.ops_type	  = 0;
	data.cgroup_root  = BPF_CORE_READ(cgrp, root, hierarchy_id);
	data.cgroup_level = BPF_CORE_READ(cgrp, level);

	/* css */
	bpf_probe_read(&data.css, sizeof(u64) * CGROUP_SUBSYS_COUNT,
		       BPF_CORE_READ(cgrp, subsys));

	/* output */
	bpf_perf_event_output(ctx, &cgroup_perf_events, BPF_F_CURRENT_CPU,
			      &data, sizeof(data));
	return 0;
}
