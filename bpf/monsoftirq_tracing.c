#include "vmlinux.h"
#include "bpf_common.h"
#include <bpf/bpf_helpers.h>
#include <bpf/bpf_tracing.h>
#include <bpf/bpf_core_read.h>

#define NSEC_PER_MSEC			1000000UL
#define NSEC_PER_USEC			1000UL
#define NR_SOFTIRQS_MAX			16 // must be 2^order

enum lat_zone {
	LAT_ZONE0=0,	// 0 ~ 10us
	LAT_ZONE1,	// 10us ~ 100us
	LAT_ZONE2,	// 100us ~ 1ms
	LAT_ZONE3,	// 1ms ~ inf
	LAT_ZONE_MAX,
};

struct tp_softirq {
	unsigned long long pad;
	unsigned int vec;
};

// Because bpf access array is strictly checked,
// the size of the array must be aligned in order
// of 2, so we should not use NR_SOFTIRQS, but
// use NR_SOFTIRQS_MAX as the size of the array
struct softirq_lat {
	u64 silat[NR_SOFTIRQS_MAX][LAT_ZONE_MAX];
};

struct {
	__uint(type, BPF_MAP_TYPE_PERCPU_ARRAY);
	//key -> NR_SOFTIRQS
	__type(key, u32);
	// value -> ts, record softirq_raise start time
	__type(value, u64);
	__uint(max_entries, NR_SOFTIRQS);
} silat_map SEC(".maps");//softirq latency map

struct {
	__uint(type, BPF_MAP_TYPE_ARRAY);
	__uint(key_size, sizeof(u32));
	__uint(value_size, sizeof(struct softirq_lat));
	__uint(max_entries, 1);
} softirq_lats SEC(".maps");

SEC("tracepoint/irq/softirq_raise")
void probe_softirq_raise(struct tp_softirq *ctx)
{
	u32 nr;
	u64 now;
	nr = ctx->vec;

	now = bpf_ktime_get_ns();
	bpf_map_update_elem(&silat_map, &nr, &now, BPF_ANY);
}

static void
calc_softirq_latency(struct softirq_lat *lat_mc, u32 nr, u64 now)
{
	u64 lat, *ts;

	ts = bpf_map_lookup_elem(&silat_map, &nr);
	if (!ts)
		return;

	lat = now - *ts;

	//update to metrics
	if (lat < 10 * NSEC_PER_USEC) { //10us
		__sync_fetch_and_add(&lat_mc->silat[nr & (NR_SOFTIRQS_MAX - 1)][LAT_ZONE0], 1);
	} else if (lat < 100 * NSEC_PER_USEC) {//100us
		__sync_fetch_and_add(&lat_mc->silat[nr & (NR_SOFTIRQS_MAX - 1)][LAT_ZONE1], 1);
	} else if (lat < 1 * NSEC_PER_MSEC) {//1ms
		__sync_fetch_and_add(&lat_mc->silat[nr & (NR_SOFTIRQS_MAX - 1)][LAT_ZONE2], 1);
	} else {//1ms+
		__sync_fetch_and_add(&lat_mc->silat[nr & (NR_SOFTIRQS_MAX - 1)][LAT_ZONE3], 1);
	}
}

SEC("tracepoint/irq/softirq_entry")
void probe_softirq_entry(struct tp_softirq *ctx)
{
	u32 key = 0, nr;
	u64 now;
	struct softirq_lat *lat_mc;

	lat_mc = bpf_map_lookup_elem(&softirq_lats, &key);
	if (!lat_mc)
		return;

	nr = ctx->vec;

	now = bpf_ktime_get_ns();

	// update softirq lat to lat metric
	calc_softirq_latency(lat_mc, nr, now);
}

char __license[] SEC("license") = "Dual MIT/GPL";
