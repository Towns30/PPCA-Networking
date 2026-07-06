# QUIC Congestion Control

[中文版](quic-cc.zh.md)

> Elective project (5') — Networking track

## Motivation

Modern proxies like **Hysteria** replace the transport's
congestion control with a custom algorithm running over QUIC. Hysteria's "Brutal"
controller deliberately ignores loss and sends at a fixed rate to punch through
lossy international links.

In this project you implement your **own QUIC congestion-control algorithm** and
tune it to maximise goodput across emulated networks — without being unfair to
competing traffic.

## What you do

You build on [`apernet/quic-go`](https://github.com/apernet/quic-go) (the fork
Hysteria uses), which exposes a pluggable congestion-control hook. You implement
one interface; the framework handles handshakes, reliability, ACKs, pacing, and
flow control.

**You only edit `internal/cc/student.go`.** Everything else is fixed.

## The interface

```go
type Controller interface {
    OnInit(maxDatagramSize int64)
    OnAck(ev AckEvent)
    OnLoss(ev LossEvent)
    CongestionWindow() int64    // bytes allowed in flight
    PacingRate() int64          // bytes/sec, or 0 to disable
}
```

`AckEvent` / `LossEvent` carry `BytesAcked`/`BytesLost`, `BytesInFlight`,
monotonic timestamp `Now`, and live RTT estimates.

## Algorithm choices

Pick one and go deep:

- **CUBIC** (RFC 8312) — window as cubic function of time since last loss
- **BBR** — estimate bottleneck bandwidth + min RTT; pace at BtlBw
- **Delay-based (Vegas / Copa)** — react to RTT inflation before loss
- **Your own design** — allowed and encouraged

## Evaluation

`testbed/run.sh` emulates networks with `tc/netem` (LAN, broadband, trans-Pacific,
lossy, bufferbloat, shallow-buffer). Each scenario measures:

1. **Utilization** — single-flow goodput ÷ bottleneck bandwidth
2. **Fairness** — Jain index alongside a competing TCP CUBIC flow

```
scenario_score = utilization × fairness
total = Σ(weight × scenario_score) / Σ(weight) × 100
```

### Reference scores

A TA implemented several congestion-control policies and ran them through the
published scorecard. These numbers are provided so you have a realistic sense of
the score scale; a score around 40 is not surprising for some algorithm you write.

| Policy | Score |
|--------|------:|
| Some optimized algorithm I implemented | 57.6 |
| `cubic` | 50.6 |
| `bbr` | 46.7 |
| `hysteria` | 43.0 |
| `reno` | 31.7 |
| `student` baseline | 26.3 |

Use these numbers as context, not as a hard target. Your understanding of the
algorithm, how you justify your algorithm, etc., is the major part of the grading.

## Deliverables

1. Your `student.go`
2. Report: algorithm, signals used, scorecard, win/loss analysis, (and why this algorithm & what inspired you)

## References

- RFC 5681 (TCP CC), RFC 6582 (NewReno), RFC 8312 (CUBIC), RFC 9002 (QUIC CC)
- [Cardwell et al., *BBR*, ACM Queue 2016](https://queue.acm.org/detail.cfm?id=3022184)
- [Arun & Balakrishnan, *Copa*, NSDI 2018](https://www.usenix.org/conference/nsdi18/presentation/arun)
- Hysteria "Brutal" controller source (`apernet/hysteria`)
