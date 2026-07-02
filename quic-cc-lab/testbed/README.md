# Testbed

Emulates a set of networks with Linux network namespaces + `tc/netem`, runs the
`bench` harness across them, and scores the result. **Linux + root required**
(`tc`, `ip netns`); fairness runs also need `iperf3`.

```
[ cccli ns ] 10.0.0.2  <== veth (netem) ==>  10.0.0.1 [ ccsrv ns ]
   bench client (rx)                            bench server (tx, CC under test)
```

The **server → client** (download) direction carries the bulk data and gets the
bottleneck qdisc: `netem delay <rtt/2> loss <p> rate <bw> limit <qlen>`. The
`limit` is the queue length in packets — this is the bufferbloat knob (≈BDP is
well-tuned; ≫BDP is a bloated buffer; ≪BDP is a shallow buffer). The reverse
direction gets `delay <rtt/2>` so the round trip is symmetric.

## Quick start

```bash
sudo ./run.sh all student                 # every scenario + fairness, then scorecard
sudo ./run.sh single lossy student /tmp/out 20
sudo ./run.sh fair   broadband student /tmp/out 20
sudo ./run.sh setup 50mbit 40 2 850       # just build the network, then poke at it
sudo ./run.sh teardown
```

Scenarios live in `scenarios.conf` (name, bandwidth, RTT, loss%, queue length,
weight) — edit freely, and keep at least one **held-out** scenario out of the
copy you give students.

## Running without a Linux host

macOS/Windows: use a Linux VM (UTM/Multipass/VirtualBox) or a privileged
container:

```bash
docker run --rm -it --privileged -v "$PWD/..":/lab -w /lab/testbed \
  golang:1.24 bash -c 'apt-get update && apt-get install -y iproute2 ethtool iperf3 && \
                       sudo ./run.sh all student'
```

`--privileged` is needed for `ip netns` and `tc`.

## Interpreting output

`bench` emits one JSON line per client run:

```json
{"cc":"student","duration_s":15.0,"bytes":88000000,"goodput_mbps":46.9,
 "steady_goodput_mbps":47.4,"intervals":[{"t":0.5,"mbps":12.1}, ...]}
```

- `goodput_mbps` — whole-run average.
- `steady_goodput_mbps` — average of the 500 ms interval samples after warmup
  (excludes handshake + slow start). Use this for utilization.
- `intervals` — the throughput trace; plot it to see ramp-up, sawtooths, and
  reaction to loss. Great material for the report.

`score.py` reads these plus the `iperf3 -J` fairness output and prints the
weighted scorecard. Re-score cached results without re-running:

```bash
python3 score.py --conf scenarios.conf --results /tmp/out --cc student
```
