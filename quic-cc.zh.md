# QUIC 拥塞控制

[English version](quic-cc.md)

> 自选项目 (5') — 网络方向

## 动机

现代代理（如 **Hysteria**）用自定义拥塞控制算法替换传输层的默认算法，运行在 QUIC 上。Hysteria 的 "Brutal" 控制器故意忽略丢包，以固定速率发送来穿透有损的国际线路。

在这个项目中，你实现**自己的 QUIC 拥塞控制算法**，在模拟网络环境下最大化吞吐量——同时不能对共享链路的其他流量不公平。

## 你要做什么

基于 [`apernet/quic-go`](https://github.com/apernet/quic-go)（Hysteria 使用的 fork）开发，它暴露了可插拔的拥塞控制钩子。你实现一个接口；框架处理握手、可靠性、ACK、pacing 和流控。

**你只编辑 `internal/cc/student.go`。** 其他文件固定不变。

## 接口

```go
type Controller interface {
    OnInit(maxDatagramSize int64)
    OnAck(ev AckEvent)
    OnLoss(ev LossEvent)
    CongestionWindow() int64    // 允许在途的字节数
    PacingRate() int64          // 字节/秒，0 表示不 pacing
}
```

`AckEvent` / `LossEvent` 包含 `BytesAcked`/`BytesLost`、`BytesInFlight`、单调时间戳 `Now`、以及实时 RTT 估计。

## 算法选择

选一个深入研究：

- **CUBIC** (RFC 8312) — 窗口是自上次丢包以来时间的三次函数
- **BBR** — 估计瓶颈带宽 + 最小 RTT；按 BtlBw 发送
- **延迟驱动 (Vegas / Copa)** — 在丢包前对 RTT 膨胀做出反应
- **自定义设计** — 允许并鼓励

## 评估

`testbed/run.sh` 用 `tc/netem` 模拟网络（LAN、宽带、跨太平洋、有损、bufferbloat、浅缓冲区）。每个场景测量：

1. **利用率** — 单流 goodput ÷ 瓶颈带宽
2. **公平性** — 与竞争的 TCP CUBIC 流并行时的 Jain 公平指数

```
scenario_score = utilization × fairness
total = Σ(weight × scenario_score) / Σ(weight) × 100
```

### 参考分数

TA 实现过若干拥塞控制策略，并在发布的 scorecard 上跑了完整评分。下面的
数字用于帮助你理解分数尺度；如果你写的某个算法拿到 40 分左右，并不奇怪。

| 策略 | 分数 |
|------|-----:|
| 我写的某个 optimized 算法 | 57.6 |
| `cubic` | 50.6 |
| `bbr` | 46.7 |
| `hysteria` | 43.0 |
| `reno` | 31.7 |
| `student` baseline | 26.3 |

这些数字仅供参考，不是硬性目标；你对算法的理解，你如何 justify 你的做法，仍然是评分的重要组成。

## 交付物

1. 你的 `student.go`
2. 报告：算法、使用的信号、scorecard、win/loss 分析，以及为什么你的算法是有效的 + 你从什么算法中获取了灵感

## 参考资料

- RFC 5681 (TCP CC), RFC 6582 (NewReno), RFC 8312 (CUBIC), RFC 9002 (QUIC CC)
- Cardwell et al., *BBR*, ACM Queue 2016
- Arun & Balakrishnan, *Copa*, NSDI 2018
- Hysteria "Brutal" 控制器源码 (`apernet/hysteria`)
