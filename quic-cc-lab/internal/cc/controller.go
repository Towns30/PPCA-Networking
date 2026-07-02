// Package cc is the pluggable congestion-control layer for the lab.
//
// You implement a single interface, [Controller], in student.go. Everything
// else in this package is plumbing that wires your algorithm into the QUIC
// sender (see adapter.go) and is off-limits for the assignment — do not edit
// controller.go, adapter.go, registry.go, or reno.go.
package cc

import "time"

// RTT exposes the connection's live round-trip-time estimates, maintained by
// the QUIC stack from ACK feedback. All methods are cheap reads and safe to
// call from inside your OnAck / OnLoss handlers. Values are 0 before the first
// RTT sample has been taken.
type RTT interface {
	// Smoothed is the smoothed RTT (SRTT, RFC 6298).
	Smoothed() time.Duration
	// Latest is the most recent individual RTT sample.
	Latest() time.Duration
	// Min is the minimum RTT seen on the connection — your best estimate of
	// the propagation delay (RTprop), used by delay-based and BBR-style CCs.
	Min() time.Duration
	// MeanDeviation is the RTT variation (RTTVAR).
	MeanDeviation() time.Duration
}

// AckEvent describes exactly one newly acknowledged, ack-eliciting packet.
// OnAck is called once per such packet, in roughly the order they were acked.
type AckEvent struct {
	// BytesAcked is the wire size of the acknowledged packet.
	BytesAcked int64
	// BytesInFlight is the number of bytes in flight *before* this ACK was
	// applied (i.e. the prior in-flight count).
	BytesInFlight int64
	// RTT gives live access to the connection's RTT estimates.
	RTT RTT
	// Now is a monotonic timestamp for this event, measured as elapsed time
	// since the connection's congestion controller was created. Use it to
	// implement per-RTT logic (loss epochs), BBR-style cycle timing, etc.
	Now time.Duration
}

// LossEvent describes exactly one packet that the sender declared lost.
// OnLoss is called once per lost packet.
type LossEvent struct {
	// BytesLost is the wire size of the lost packet.
	BytesLost int64
	// BytesInFlight is the number of bytes in flight before this loss was
	// applied.
	BytesInFlight int64
	RTT           RTT
	Now           time.Duration
}

// Controller is the congestion-control algorithm you implement.
//
// The QUIC sender drives it like this, on the connection's hot path:
//   - OnInit once, before the first packet is sent.
//   - OnAck / OnLoss as ACKs and loss declarations arrive.
//   - Before every send opportunity it reads CongestionWindow() to decide how
//     many bytes may be in flight, and PacingRate() to decide how fast to emit
//     them.
//
// Keep every method cheap, non-blocking, and allocation-free. Do not spawn
// goroutines or take locks that can block — you are running inside the QUIC
// event loop.
type Controller interface {
	// OnInit is called exactly once, before any packet is sent, with the
	// maximum datagram (packet) size in bytes (typically ~1200). Size your
	// initial congestion window here; a common choice is 10 * maxDatagramSize.
	OnInit(maxDatagramSize int64)

	// OnAck is called once per newly acknowledged ack-eliciting packet.
	OnAck(ev AckEvent)

	// OnLoss is called once per packet declared lost.
	OnLoss(ev LossEvent)

	// CongestionWindow returns the current window in bytes. The sender keeps at
	// most this many bytes in flight. The adapter floors the effective window
	// at one packet, so returning 0 will not deadlock the connection.
	CongestionWindow() int64

	// PacingRate returns the pacing rate in bytes per second, used to spread
	// the congestion window across an RTT so packets are not sent in one burst.
	// Return 0 to disable pacing (the sender is then limited only by the
	// congestion window). Note this method takes no arguments: if your pacing
	// rate depends on RTT, cache the RTT you need from the last AckEvent.
	PacingRate() int64
}

// TimeoutHandler is an OPTIONAL interface. If your Controller also implements
// it, OnRetransmissionTimeout is called whenever the loss-detection timer
// (PTO/RTO) fires. packetsLost reports whether any packets were actually
// retransmitted as a result. Classic TCP collapses the window to one packet
// here; implementing this is not required to pass, but helps under heavy loss.
type TimeoutHandler interface {
	OnRetransmissionTimeout(packetsLost bool)
}

// StateReporter is an OPTIONAL interface used purely for telemetry/logging
// (the bench and qlog may report these). It has no effect on sending.
type StateReporter interface {
	InSlowStart() bool
	InRecovery() bool
}
