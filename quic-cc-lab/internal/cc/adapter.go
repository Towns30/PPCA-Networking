package cc

// adapter.go bridges a student Controller to apernet/quic-go's
// congestion.CongestionControl interface. DO NOT EDIT for the assignment.
//
// The QUIC sender interacts with a congestion.CongestionControl through ~15
// methods that speak in monotonic time and quic-go byte-count types. This file
// collapses that surface down to the small Controller interface students
// implement, and owns a token-bucket pacer so students only need to return a
// pacing *rate*.
//
// A note on callbacks: this adapter deliberately implements only the base
// congestion.CongestionControl interface (NOT CongestionControlEx). quic-go
// therefore delivers per-packet OnPacketAcked / OnCongestionEvent callbacks
// (one call per acked/lost packet) rather than the batched Ex variant, which
// keeps the student-facing model simple and avoids double counting.

import (
	"time"

	"github.com/apernet/quic-go/congestion"
	"github.com/apernet/quic-go/monotime"
)

// compile-time assertion: we satisfy the base interface (and not the Ex one).
var _ congestion.CongestionControl = (*adapter)(nil)

type adapter struct {
	ctrl  Controller
	rtt   *rttProvider
	pacer *pacer

	maxDatagramSize congestion.ByteCount
	started         bool
	startTime       monotime.Time
}

func newAdapter(ctrl Controller) *adapter {
	a := &adapter{
		ctrl:            ctrl,
		rtt:             &rttProvider{},
		maxDatagramSize: congestion.InitialPacketSize,
	}
	a.pacer = newPacer(func() congestion.ByteCount {
		r := a.ctrl.PacingRate()
		if r <= 0 {
			return 0
		}
		return congestion.ByteCount(r)
	}, a.maxDatagramSize)
	return a
}

// ensureStarted lazily fires OnInit exactly once, before the controller sees
// any other callback, and stamps the monotonic epoch used for AckEvent.Now.
func (a *adapter) ensureStarted() {
	if a.started {
		return
	}
	a.started = true
	a.startTime = monotime.Now()
	a.ctrl.OnInit(int64(a.maxDatagramSize))
}

func (a *adapter) SetRTTStatsProvider(p congestion.RTTStatsProvider) {
	a.rtt.p = p
}

func (a *adapter) SetMaxDatagramSize(size congestion.ByteCount) {
	a.maxDatagramSize = size
	a.pacer.setMaxDatagramSize(size)
	a.ensureStarted()
}

func (a *adapter) CanSend(bytesInFlight congestion.ByteCount) bool {
	a.ensureStarted()
	return bytesInFlight < a.GetCongestionWindow()
}

func (a *adapter) GetCongestionWindow() congestion.ByteCount {
	a.ensureStarted()
	w := a.ctrl.CongestionWindow()
	if w < int64(a.maxDatagramSize) {
		// Floor at one packet so a buggy/zero window cannot deadlock the
		// connection.
		w = int64(a.maxDatagramSize)
	}
	return congestion.ByteCount(w)
}

func (a *adapter) OnPacketSent(sentTime monotime.Time, bytesInFlight congestion.ByteCount, packetNumber congestion.PacketNumber, bytes congestion.ByteCount, isRetransmittable bool) {
	a.ensureStarted()
	a.pacer.sentPacket(sentTime, bytes)
}

func (a *adapter) OnPacketAcked(number congestion.PacketNumber, ackedBytes congestion.ByteCount, priorInFlight congestion.ByteCount, eventTime monotime.Time) {
	a.ensureStarted()
	a.ctrl.OnAck(AckEvent{
		BytesAcked:    int64(ackedBytes),
		BytesInFlight: int64(priorInFlight),
		RTT:           a.rtt,
		Now:           eventTime.Sub(a.startTime),
	})
}

func (a *adapter) OnCongestionEvent(number congestion.PacketNumber, lostBytes congestion.ByteCount, priorInFlight congestion.ByteCount) {
	a.ensureStarted()
	if lostBytes == 0 {
		// lostBytes == 0 is an ECN-CE congestion signal, not a packet loss.
		// This lab does not use ECN; ignore it.
		return
	}
	a.ctrl.OnLoss(LossEvent{
		BytesLost:     int64(lostBytes),
		BytesInFlight: int64(priorInFlight),
		RTT:           a.rtt,
		Now:           monotime.Now().Sub(a.startTime),
	})
}

func (a *adapter) OnRetransmissionTimeout(packetsRetransmitted bool) {
	if th, ok := a.ctrl.(TimeoutHandler); ok {
		th.OnRetransmissionTimeout(packetsRetransmitted)
	}
}

// MaybeExitSlowStart is a hint from quic-go. Students manage their own slow
// start via their window/ssthresh, so this is a no-op.
func (a *adapter) MaybeExitSlowStart() {}

func (a *adapter) HasPacingBudget(now monotime.Time) bool {
	if a.ctrl.PacingRate() <= 0 {
		return true // pacing disabled: only the congestion window limits sending
	}
	return a.pacer.budget(now) >= a.maxDatagramSize
}

func (a *adapter) TimeUntilSend(bytesInFlight congestion.ByteCount) monotime.Time {
	if a.ctrl.PacingRate() <= 0 {
		return 0 // send as soon as the window allows
	}
	return a.pacer.timeUntilSend()
}

func (a *adapter) InSlowStart() bool {
	if r, ok := a.ctrl.(StateReporter); ok {
		return r.InSlowStart()
	}
	return false
}

func (a *adapter) InRecovery() bool {
	if r, ok := a.ctrl.(StateReporter); ok {
		return r.InRecovery()
	}
	return false
}

// rttProvider adapts quic-go's RTTStatsProvider to the small RTT interface.
type rttProvider struct{ p congestion.RTTStatsProvider }

func (r *rttProvider) Smoothed() time.Duration {
	if r.p == nil {
		return 0
	}
	return r.p.SmoothedRTT()
}
func (r *rttProvider) Latest() time.Duration {
	if r.p == nil {
		return 0
	}
	return r.p.LatestRTT()
}
func (r *rttProvider) Min() time.Duration {
	if r.p == nil {
		return 0
	}
	return r.p.MinRTT()
}
func (r *rttProvider) MeanDeviation() time.Duration {
	if r.p == nil {
		return 0
	}
	return r.p.MeanDeviation()
}
