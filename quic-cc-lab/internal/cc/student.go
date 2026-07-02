package cc

// ============================================================================
//  THIS IS THE ONLY FILE YOU EDIT.
// ============================================================================
//
// Implement your congestion-control algorithm here by filling in the methods of
// `student`. It is registered below under the name "student"; run any binary
// with `-cc student` to use it.
//
// What you get (see controller.go for full docs):
//   - OnInit(maxDatagramSize)      once, before the first packet is sent.
//   - OnAck(ev)                    per acknowledged packet; ev.RTT has live
//                                  Smoothed/Latest/Min/MeanDeviation RTTs, and
//                                  ev.BytesInFlight / ev.Now for timing logic.
//   - OnLoss(ev)                   per lost packet.
//   - CongestionWindow()           bytes allowed in flight (floored at 1 pkt).
//   - PacingRate()                 bytes/sec; return 0 to disable pacing.
//
// Optional interfaces you MAY also implement on `student` (see controller.go):
//   - TimeoutHandler.OnRetransmissionTimeout(packetsLost bool)
//   - StateReporter.InSlowStart() / InRecovery()   (telemetry only)
//
// The baseline below is a DELIBERATELY NAÏVE AIMD controller: no slow start, no
// pacing, halve-on-loss. It works, but it ramps up slowly, reacts poorly to
// bursty loss, and leaves throughput on the table over high-BDP and lossy
// links. Your job is to replace it with a real algorithm (e.g. CUBIC, BBR, a
// delay-based scheme like Vegas/Copa, or your own design tuned for the proxy
// scenarios in testbed/). See README.md §"What to implement".
//
// Rules:
//   - Do not edit any other file in internal/cc/.
//   - Keep every method fast, non-blocking, and allocation-free.
//   - Do not call time.Now(): use ev.Now (monotonic, per-connection) instead.

import "time"

func init() {
	Register("student", func() Controller { return &student{} })
}

type student struct {
	mds  int64
	cwnd int64

	// tracks progress toward the next additive increase
	acked       int64
	recoveryEnd time.Duration
}

func (s *student) OnInit(maxDatagramSize int64) {
	s.mds = maxDatagramSize
	s.cwnd = 4 * maxDatagramSize // small, conservative start (no slow start)
}

func (s *student) OnAck(ev AckEvent) {
	// Additive increase: +1 MSS per window of acknowledged data.
	s.acked += ev.BytesAcked
	if s.acked >= s.cwnd {
		s.acked -= s.cwnd
		s.cwnd += s.mds
	}
	// TODO: add slow start, RTT-aware growth (CUBIC), or rate estimation (BBR).
}

func (s *student) OnLoss(ev LossEvent) {
	if ev.Now < s.recoveryEnd {
		return
	}
	// Multiplicative decrease.
	s.cwnd /= 2
	if s.cwnd < 2*s.mds {
		s.cwnd = 2 * s.mds
	}
	s.acked = 0

	epoch := ev.RTT.Smoothed()
	if epoch <= 0 {
		epoch = 100 * time.Millisecond
	}
	s.recoveryEnd = ev.Now + epoch
	// TODO: distinguish congestion loss from random loss; consider ev.RTT.
}

func (s *student) CongestionWindow() int64 { return s.cwnd }

func (s *student) PacingRate() int64 {
	// TODO: pacing markedly improves fairness and loss behaviour. A common
	// choice is ~1.25 * cwnd / SRTT (cache SRTT from OnAck, since this method
	// takes no arguments). Returning 0 disables pacing.
	return 0
}
