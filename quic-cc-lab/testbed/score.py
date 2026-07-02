#!/usr/bin/env python3
"""Score a congestion controller from testbed results.

Reads the per-scenario single-flow result (how well it fills the pipe) and the
fairness result (how it shares with a competing TCP CUBIC flow), and combines
them into a weighted score.

    scenario_score = utilization * fairness            (each in [0, 1])
    total = sum(weight * scenario_score) / sum(weight) * 100

Utilization rewards filling the bottleneck. Fairness is the Jain index between
the student's flow and the TCP flow: it is maximal (1.0) at an even split and
drops for BOTH starvation and over-aggression, so a "just blast packets" design
(e.g. Brutal) that starves the TCP flow is penalised rather than rewarded.
"""
import argparse
import glob
import json
import os
import sys


def parse_rate_mbps(s):
    s = s.strip().lower()
    for suf, mul in (("gbit", 1000.0), ("mbit", 1.0), ("kbit", 0.001)):
        if s.endswith(suf):
            return float(s[: -len(suf)]) * mul
    return float(s)  # assume Mbps


def load_scenarios(conf):
    rows = []
    with open(conf) as f:
        for line in f:
            line = line.strip()
            if not line or line.startswith("#"):
                continue
            name, bw, rtt, loss, qlen, weight = line.split()
            rows.append(
                dict(name=name, bw=parse_rate_mbps(bw), rtt=float(rtt),
                     loss=float(loss), qlen=int(qlen), weight=float(weight))
            )
    return rows


def jain(x, y):
    if x <= 0 and y <= 0:
        return 0.0
    return (x + y) ** 2 / (2 * (x * x + y * y))


def read_json(path):
    try:
        with open(path) as f:
            return json.load(f)
    except (OSError, ValueError):
        return None


def iperf_mbps(path):
    d = read_json(path)
    if not d:
        return None
    try:
        return d["end"]["sum_received"]["bits_per_second"] / 1e6
    except (KeyError, TypeError):
        return None


def main():
    ap = argparse.ArgumentParser()
    ap.add_argument("--conf", required=True)
    ap.add_argument("--results", required=True)
    ap.add_argument("--cc", required=True)
    args = ap.parse_args()

    scenarios = load_scenarios(args.conf)
    total_w = sum(s["weight"] for s in scenarios)
    total = 0.0

    print(f"\n=== congestion-control scorecard: cc={args.cc} ===")
    hdr = f"{'scenario':<14}{'bw':>7}{'goodput':>9}{'util':>7}{'quic':>8}{'tcp':>8}{'jain':>7}{'score':>8}"
    print(hdr)
    print("-" * len(hdr))

    for s in scenarios:
        base = os.path.join(args.results, f"{s['name']}.{args.cc}")

        single = read_json(base + ".json")
        goodput = single.get("steady_goodput_mbps") or single.get("goodput_mbps") if single else 0.0
        goodput = goodput or 0.0
        util = min(1.0, goodput / s["bw"]) if s["bw"] > 0 else 0.0

        quic = read_json(base + ".fair.quic.json")
        quic_bw = (quic.get("goodput_mbps") if quic else None) or 0.0
        tcp_bw = iperf_mbps(base + ".fair.tcp.json") or 0.0
        if quic_bw > 0 or tcp_bw > 0:
            fair = jain(quic_bw, tcp_bw)
        else:
            fair = 1.0  # no fairness data: don't penalise

        sc = util * fair
        total += s["weight"] * sc
        print(f"{s['name']:<14}{s['bw']:>7.0f}{goodput:>9.1f}{util:>7.2f}"
              f"{quic_bw:>8.1f}{tcp_bw:>8.1f}{fair:>7.2f}{100*sc:>8.1f}")

    final = 100 * total / total_w if total_w else 0.0
    print("-" * len(hdr))
    print(f"{'WEIGHTED TOTAL':<{len(hdr)-8}}{final:>8.1f}")
    print()
    return 0 if final > 0 else 1


if __name__ == "__main__":
    sys.exit(main())
