#!/usr/bin/env bash
# Congestion-control testbed harness (Linux only: uses network namespaces + tc).
#
# Topology: two netns joined by a veth pair, with tc/netem emulating the path.
#
#   [ cli ns ]  10.0.0.2  <== veth ==>  10.0.0.1  [ srv ns ]
#        bench client                    bench server (sender, CC under test)
#
# Usage:
#   sudo ./run.sh all       <cc> [outdir] [duration]   # every scenario + fairness, then score
#   sudo ./run.sh single    <scenario> <cc> <outdir> [duration]
#   sudo ./run.sh fair      <scenario> <cc> <outdir> [duration]
#   sudo ./run.sh setup     <bw> <rtt_ms> <loss_pct> <qlen>   # just build the network (debug)
#   sudo ./run.sh teardown
#
# Requires: root, iproute2 (ip/tc), and for fairness runs, iperf3.
set -euo pipefail

HERE="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPO="$(cd "$HERE/.." && pwd)"
BENCH="$REPO/bin/bench"
CONF="$HERE/scenarios.conf"

SRV_NS=ccsrv
CLI_NS=cccli
SRV_IP=10.0.0.1
CLI_IP=10.0.0.2
PORT=4433

need_root() { [[ $EUID -eq 0 ]] || { echo "must run as root (sudo)"; exit 1; }; }

build() {
  [[ -x "$BENCH" ]] && return
  echo "building bench ..."
  ( cd "$REPO" && go build -o "$BENCH" ./cmd/bench )
}

teardown() {
  ip netns del "$SRV_NS" 2>/dev/null || true
  ip netns del "$CLI_NS" 2>/dev/null || true
}

# setup <bw> <rtt_ms> <loss_pct> <qlen>
setup() {
  local bw=$1 rtt=$2 loss=$3 qlen=$4
  local owd=$(( rtt / 2 ))
  teardown

  ip netns add "$SRV_NS"
  ip netns add "$CLI_NS"
  ip link add vsrv netns "$SRV_NS" type veth peer name vcli netns "$CLI_NS"

  ip -n "$SRV_NS" addr add "$SRV_IP/24" dev vsrv
  ip -n "$CLI_NS" addr add "$CLI_IP/24" dev vcli
  ip -n "$SRV_NS" link set vsrv up
  ip -n "$CLI_NS" link set vcli up
  ip -n "$SRV_NS" link set lo up
  ip -n "$CLI_NS" link set lo up

  # Disable offloads so netem rate/loss accounting reflects real packets.
  for ns_dev in "$SRV_NS vsrv" "$CLI_NS vcli"; do
    set -- $ns_dev
    ip netns exec "$1" ethtool -K "$2" gso off gro off tso off 2>/dev/null || true
  done

  # Download path (server -> client): the bottleneck. rate + delay + loss + buffer.
  ip netns exec "$SRV_NS" tc qdisc replace dev vsrv root netem \
      delay "${owd}ms" loss "${loss}%" rate "$bw" limit "$qlen"
  # Reverse path (client -> server): delay only, so RTT = 2*owd.
  ip netns exec "$CLI_NS" tc qdisc replace dev vcli root netem delay "${owd}ms"
}

# read a scenario row from the conf into globals: S_BW S_RTT S_LOSS S_QLEN S_WEIGHT
load_scenario() {
  local want=$1 name bw rtt loss qlen weight
  while read -r name bw rtt loss qlen weight; do
    [[ -z "$name" || "$name" == \#* ]] && continue
    if [[ "$name" == "$want" ]]; then
      S_BW=$bw; S_RTT=$rtt; S_LOSS=$loss; S_QLEN=$qlen; S_WEIGHT=$weight
      return 0
    fi
  done < "$CONF"
  echo "unknown scenario: $want"; exit 1
}

# single <scenario> <cc> <outdir> [duration]
single() {
  local scen=$1 cc=$2 outdir=$3 dur=${4:-15}
  load_scenario "$scen"
  build; mkdir -p "$outdir"
  setup "$S_BW" "$S_RTT" "$S_LOSS" "$S_QLEN"

  ip netns exec "$SRV_NS" "$BENCH" -mode server -listen "$SRV_IP:$PORT" -cc "$cc" &
  local srv=$!; sleep 0.5
  ip netns exec "$CLI_NS" "$BENCH" -mode client -server "$SRV_IP:$PORT" \
      -cc "$cc" -duration "${dur}s" -json > "$outdir/$scen.$cc.json"
  kill "$srv" 2>/dev/null || true; wait "$srv" 2>/dev/null || true
  teardown
  echo "  [$scen] $(grep -o '\"goodput_mbps\":[0-9.]*' "$outdir/$scen.$cc.json")"
}

# fair <scenario> <cc> <outdir> [duration]: student QUIC flow vs one TCP CUBIC flow.
fair() {
  local scen=$1 cc=$2 outdir=$3 dur=${4:-20}
  command -v iperf3 >/dev/null || { echo "iperf3 required for fairness runs"; exit 1; }
  load_scenario "$scen"
  build; mkdir -p "$outdir"
  setup "$S_BW" "$S_RTT" "$S_LOSS" "$S_QLEN"

  ip netns exec "$SRV_NS" "$BENCH" -mode server -listen "$SRV_IP:$PORT" -cc "$cc" &
  local bsrv=$!
  ip netns exec "$SRV_NS" iperf3 -s -1 >/dev/null 2>&1 &
  local isrv=$!; sleep 0.5

  # Launch both flows together; they share the bottleneck.
  ip netns exec "$CLI_NS" "$BENCH" -mode client -server "$SRV_IP:$PORT" \
      -cc "$cc" -duration "${dur}s" -json > "$outdir/$scen.$cc.fair.quic.json" &
  local bcli=$!
  ip netns exec "$CLI_NS" iperf3 -c "$SRV_IP" -C cubic -t "$dur" -J \
      > "$outdir/$scen.$cc.fair.tcp.json" 2>/dev/null &
  local icli=$!
  wait "$bcli" || true; wait "$icli" || true
  kill "$bsrv" "$isrv" 2>/dev/null || true; wait 2>/dev/null || true
  teardown
  echo "  [$scen/fair] done"
}

# all <cc> [outdir] [duration]
all() {
  local cc=$1 outdir=${2:-"$HERE/results/$cc"} dur=${3:-15}
  mkdir -p "$outdir"
  while read -r name _; do
    [[ -z "$name" || "$name" == \#* ]] && continue
    single "$name" "$cc" "$outdir" "$dur"
    fair   "$name" "$cc" "$outdir" "$dur" || echo "  (skipped fairness for $name)"
  done < "$CONF"
  echo
  python3 "$HERE/score.py" --conf "$CONF" --results "$outdir" --cc "$cc"
}

need_root
cmd=${1:-}; shift || true
case "$cmd" in
  setup)    setup "$@";;
  teardown) teardown;;
  single)   single "$@";;
  fair)     fair "$@";;
  all)      all "$@";;
  *) echo "usage: sudo $0 {all|single|fair|setup|teardown} ..."; exit 1;;
esac
