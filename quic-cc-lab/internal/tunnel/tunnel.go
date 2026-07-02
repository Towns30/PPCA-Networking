// Package tunnel holds the shared QUIC + TLS setup used by the proxy and the
// benchmark. Students generally do not need to edit this; the interesting part
// of the assignment lives in internal/cc.
//
// One detail worth understanding (see README §"Flow control vs congestion
// control"): the QUIC flow-control receive windows below are set deliberately
// large. If they were small, the *receiver's* flow control — not your
// congestion controller — would cap throughput, and the experiment would
// measure the wrong thing. Large windows make the congestion window the binding
// constraint.
package tunnel

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"time"

	quic "github.com/apernet/quic-go"
)

// ALPN is the application-layer protocol name negotiated over TLS.
const ALPN = "quic-cc-lab"

// QUICConfig returns the quic-go configuration shared by both ends. The large
// receive windows keep congestion control (not flow control) the bottleneck.
func QUICConfig() *quic.Config {
	const big = 128 << 20 // 128 MiB
	return &quic.Config{
		MaxIdleTimeout:                 30 * time.Second,
		KeepAlivePeriod:                5 * time.Second,
		InitialStreamReceiveWindow:     16 << 20,
		MaxStreamReceiveWindow:         big,
		InitialConnectionReceiveWindow: 16 << 20,
		MaxConnectionReceiveWindow:     big,
	}
}

// Listen starts a QUIC listener on addr (host:port, UDP).
func Listen(addr string) (*quic.Listener, error) {
	tlsConf, err := selfSignedTLS()
	if err != nil {
		return nil, err
	}
	return quic.ListenAddr(addr, tlsConf, QUICConfig())
}

// Dial establishes a QUIC connection to addr. TLS verification is disabled
// because the lab uses throwaway self-signed certificates — do not copy this
// into production code.
func Dial(ctx context.Context, addr string) (*quic.Conn, error) {
	tlsConf := &tls.Config{
		InsecureSkipVerify: true,
		NextProtos:         []string{ALPN},
	}
	return quic.DialAddr(ctx, addr, tlsConf, QUICConfig())
}

// selfSignedTLS builds a throwaway self-signed certificate for the server.
func selfSignedTLS() (*tls.Config, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}
	tmpl := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, &tmpl, &tmpl, &key.PublicKey, key)
	if err != nil {
		return nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, err
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	cert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   []string{ALPN},
	}, nil
}
