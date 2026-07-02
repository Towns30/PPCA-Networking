package cc

// pacer is a token-bucket packet pacer, adapted from the pacer quic-go /
// Hysteria use internally. It converts a target rate (bytes/second, supplied by
// the student's PacingRate) into "may I send now?" / "when may I next send?"
// decisions. DO NOT EDIT for the assignment.

import (
	"time"

	"github.com/apernet/quic-go/congestion"
	"github.com/apernet/quic-go/monotime"
)

const (
	maxBurstPackets               = 10
	maxBurstPacingDelayMultiplier = 4
)

type pacer struct {
	budgetAtLastSent congestion.ByteCount
	maxDatagramSize  congestion.ByteCount
	lastSentTime     monotime.Time
	getBandwidth     func() congestion.ByteCount // bytes/second, may be 0
}

func newPacer(getBandwidth func() congestion.ByteCount, maxDatagramSize congestion.ByteCount) *pacer {
	return &pacer{
		budgetAtLastSent: maxBurstPackets * congestion.InitialPacketSize,
		maxDatagramSize:  maxDatagramSize,
		getBandwidth:     getBandwidth,
	}
}

func (p *pacer) sentPacket(sendTime monotime.Time, size congestion.ByteCount) {
	budget := p.budget(sendTime)
	if size > budget {
		p.budgetAtLastSent = 0
	} else {
		p.budgetAtLastSent = budget - size
	}
	p.lastSentTime = sendTime
}

func (p *pacer) budget(now monotime.Time) congestion.ByteCount {
	if p.lastSentTime.IsZero() {
		return p.maxBurstSize()
	}
	bw := p.getBandwidth()
	budget := p.budgetAtLastSent + (bw*congestion.ByteCount(now.Sub(p.lastSentTime).Nanoseconds()))/1e9
	if budget < 0 { // overflow guard
		budget = congestion.ByteCount(1<<62 - 1)
	}
	return min(p.maxBurstSize(), budget)
}

func (p *pacer) maxBurstSize() congestion.ByteCount {
	return max(
		congestion.ByteCount((maxBurstPacingDelayMultiplier*congestion.MinPacingDelay).Nanoseconds())*p.getBandwidth()/1e9,
		maxBurstPackets*p.maxDatagramSize,
	)
}

// timeUntilSend returns when the next packet may be sent. The zero value means
// "send immediately".
func (p *pacer) timeUntilSend() monotime.Time {
	if p.budgetAtLastSent >= p.maxDatagramSize {
		return 0
	}
	bw := uint64(p.getBandwidth())
	if bw == 0 {
		return 0
	}
	diff := 1e9 * uint64(p.maxDatagramSize-p.budgetAtLastSent)
	d := diff / bw
	if diff%bw > 0 { // integer math.Ceil
		d++
	}
	return p.lastSentTime.Add(max(congestion.MinPacingDelay, time.Duration(d)*time.Nanosecond))
}

func (p *pacer) setMaxDatagramSize(s congestion.ByteCount) {
	p.maxDatagramSize = s
}
