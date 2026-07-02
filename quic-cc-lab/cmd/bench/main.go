// bench is the measurement harness the testbed runs. It performs a single
// bulk download over the QUIC tunnel (server -> client) and reports goodput,
// using the same tunnel + congestion-control plumbing as the proxy. Keeping the
// benchmark separate from the proxy makes grading deterministic: no origin
// server, no SOCKS negotiation, just one long stream governed by the CC under
// test.
//
//	bench -mode server -listen :4433 -cc student
//	bench -mode client -server 10.0.0.1:4433 -duration 15 -json
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"time"

	"quic-cc-lab/internal/cc"
	"quic-cc-lab/internal/tunnel"

	quic "github.com/apernet/quic-go"
)

func main() {
	mode := flag.String("mode", "", "server | client")
	listen := flag.String("listen", ":4433", "server: QUIC listen address")
	server := flag.String("server", "127.0.0.1:4433", "client: server QUIC address")
	ccName := flag.String("cc", "reno", "congestion control algorithm (sender side)")
	duration := flag.Duration("duration", 15*time.Second, "client: measurement duration")
	warmup := flag.Duration("warmup", 2*time.Second, "client: initial time excluded from steady-state goodput")
	asJSON := flag.Bool("json", false, "client: emit a single JSON line")
	flag.Parse()

	switch *mode {
	case "server":
		runServer(*listen, *ccName)
	case "client":
		runClient(*server, *ccName, *duration, *warmup, *asJSON)
	default:
		log.Fatalf("-mode must be server or client")
	}
}

// ---- server: streams data forever on each stream ----

func runServer(listen, ccName string) {
	ln, err := tunnel.Listen(listen)
	if err != nil {
		log.Fatalf("listen: %v", err)
	}
	log.Printf("bench server on %s, cc=%s", listen, ccName)
	for {
		conn, err := ln.Accept(context.Background())
		if err != nil {
			log.Printf("accept: %v", err)
			continue
		}
		if err := cc.Apply(conn, ccName); err != nil {
			log.Fatalf("apply cc: %v", err)
		}
		go func(conn *quic.Conn) {
			for {
				stream, err := conn.AcceptStream(context.Background())
				if err != nil {
					return
				}
				go serveStream(stream)
			}
		}(conn)
	}
}

func serveStream(stream *quic.Stream) {
	// Wait for the client's 1-byte "go" trigger.
	trigger := make([]byte, 1)
	if _, err := stream.Read(trigger); err != nil {
		return
	}
	buf := make([]byte, 128<<10) // 128 KiB write chunks
	for i := range buf {
		buf[i] = byte(i)
	}
	for {
		if _, err := stream.Write(buf); err != nil {
			return // client went away; QUIC flow/congestion control paced us until now
		}
	}
}

// ---- client: reads for `duration`, samples goodput each 500ms ----

type interval struct {
	T    float64 `json:"t"`    // seconds since start
	Mbps float64 `json:"mbps"` // goodput over this interval
}

type report struct {
	CC                string     `json:"cc"`
	DurationS         float64    `json:"duration_s"`
	WarmupS           float64    `json:"warmup_s"`
	Bytes             int64      `json:"bytes"`
	GoodputMbps       float64    `json:"goodput_mbps"`        // whole run
	SteadyGoodputMbps float64    `json:"steady_goodput_mbps"` // excluding warmup
	Intervals         []interval `json:"intervals"`
}

func runClient(server, ccName string, duration, warmup time.Duration, asJSON bool) {
	conn, err := tunnel.Dial(context.Background(), server)
	if err != nil {
		log.Fatalf("dial: %v", err)
	}
	if err := cc.Apply(conn, ccName); err != nil {
		log.Fatalf("apply cc: %v", err)
	}
	stream, err := conn.OpenStreamSync(context.Background())
	if err != nil {
		log.Fatalf("open stream: %v", err)
	}
	if _, err := stream.Write([]byte{1}); err != nil {
		log.Fatalf("trigger: %v", err)
	}

	buf := make([]byte, 256<<10)
	start := time.Now()
	end := start.Add(duration)
	_ = stream.SetReadDeadline(end)

	var total int64
	var intervals []interval
	nextSample := start.Add(500 * time.Millisecond)
	var lastBytes int64
	lastT := start

	for {
		n, err := stream.Read(buf)
		total += int64(n)
		now := time.Now()
		if now.After(nextSample) || err != nil {
			dt := now.Sub(lastT).Seconds()
			if dt > 0 {
				intervals = append(intervals, interval{
					T:    now.Sub(start).Seconds(),
					Mbps: float64(total-lastBytes) * 8 / 1e6 / dt,
				})
			}
			lastBytes = total
			lastT = now
			nextSample = now.Add(500 * time.Millisecond)
		}
		if err != nil {
			break
		}
	}

	elapsed := time.Since(start).Seconds()
	rep := report{
		CC:          ccName,
		DurationS:   elapsed,
		WarmupS:     warmup.Seconds(),
		Bytes:       total,
		GoodputMbps: float64(total) * 8 / 1e6 / elapsed,
		Intervals:   intervals,
	}
	rep.SteadyGoodputMbps = steadyGoodput(intervals, warmup.Seconds())

	stream.CancelRead(0)
	conn.CloseWithError(0, "done")

	if asJSON {
		enc := json.NewEncoder(os.Stdout)
		_ = enc.Encode(rep)
	} else {
		fmt.Printf("cc=%s  bytes=%d  goodput=%.2f Mbps  steady=%.2f Mbps  (%.1fs)\n",
			rep.CC, rep.Bytes, rep.GoodputMbps, rep.SteadyGoodputMbps, rep.DurationS)
	}
}

// steadyGoodput averages the per-interval samples after the warmup window,
// giving a steady-state figure that excludes slow start / handshake.
func steadyGoodput(intervals []interval, warmupS float64) float64 {
	var sum float64
	var n int
	for _, iv := range intervals {
		if iv.T < warmupS {
			continue
		}
		sum += iv.Mbps
		n++
	}
	if n == 0 {
		return 0
	}
	return sum / float64(n)
}
