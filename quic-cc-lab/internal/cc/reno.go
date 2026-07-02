package cc

// reno is a REFERENCE congestion controller: textbook NewReno (RFC 5681 /
// RFC 6582) adapted for QUIC. It is provided complete, as a worked example of
// the Controller interface — read it, run it, then beat it. DO NOT EDIT; write
// your algorithm in student.go instead.
//
// It implements the optional TimeoutHandler and StateReporter interfaces too,
// so you can see how those hook in.

import "time"

func init() {
	Register("reno", func() Controller { return &reno{} })
}

type reno struct {
	mds      int64 // max datagram size (bytes)
	cwnd     int64 // congestion window (bytes)
	ssthresh int64 // slow-start threshold (bytes)

	// acked accumulates acked bytes during congestion avoidance; each time it
	// reaches a full window we grow cwnd by one packet (additive increase).
	acked int64

	// recoveryEnd marks the end of the current loss epoch. Losses reported
	// before this time belong to a reduction we already made, so we ignore
	// them — this gives "at most one halving per RTT".
	recoveryEnd time.Duration
	inRecovery  bool

	srtt time.Duration // cached from the last ack, for PacingRate
}

const infiniteSSThresh = int64(1) << 40

func (r *reno) OnInit(maxDatagramSize int64) {
	r.mds = maxDatagramSize
	r.cwnd = 10 * maxDatagramSize // RFC 6928 initial window
	r.ssthresh = infiniteSSThresh
}

func (r *reno) OnAck(ev AckEvent) {
	r.srtt = ev.RTT.Smoothed()

	if r.inRecovery && ev.Now >= r.recoveryEnd {
		r.inRecovery = false
	}

	if r.cwnd < r.ssthresh {
		// Slow start: exponential growth, +1 MSS worth per acked byte.
		r.cwnd += ev.BytesAcked
		return
	}
	// Congestion avoidance: additive increase, +1 MSS per cwnd of acked bytes.
	r.acked += ev.BytesAcked
	if r.acked >= r.cwnd {
		r.acked -= r.cwnd
		r.cwnd += r.mds
	}
}

func (r *reno) OnLoss(ev LossEvent) {
	if ev.Now < r.recoveryEnd {
		return // already reacted to a loss in this epoch
	}
	// Multiplicative decrease.
	r.ssthresh = r.cwnd / 2
	if min := 2 * r.mds; r.ssthresh < min {
		r.ssthresh = min
	}
	r.cwnd = r.ssthresh
	r.acked = 0
	r.inRecovery = true

	// The epoch lasts one RTT; further losses within it are the same event.
	epoch := ev.RTT.Smoothed()
	if epoch <= 0 {
		epoch = 100 * time.Millisecond
	}
	r.recoveryEnd = ev.Now + epoch
}

// OnRetransmissionTimeout implements TimeoutHandler: on RTO, collapse to a
// minimal window and re-enter slow start, like TCP.
func (r *reno) OnRetransmissionTimeout(packetsLost bool) {
	if !packetsLost {
		return
	}
	r.ssthresh = r.cwnd / 2
	if min := 2 * r.mds; r.ssthresh < min {
		r.ssthresh = min
	}
	r.cwnd = r.mds
	r.acked = 0
}

func (r *reno) CongestionWindow() int64 { return r.cwnd }

// PacingRate paces at 2x cwnd/SRTT during slow start (to let the window grow)
// and 1.25x in congestion avoidance, a common heuristic. Returns 0 (no pacing)
// until we have an RTT sample.
func (r *reno) PacingRate() int64 {
	if r.srtt <= 0 {
		return 0
	}
	num := r.cwnd * int64(time.Second)
	rate := num / int64(r.srtt)
	if r.InSlowStart() {
		return 2 * rate
	}
	return rate * 5 / 4
}

// StateReporter (telemetry only).
func (r *reno) InSlowStart() bool { return r.cwnd < r.ssthresh }
func (r *reno) InRecovery() bool  { return r.inRecovery }
