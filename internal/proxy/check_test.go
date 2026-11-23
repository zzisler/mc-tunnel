package proxy

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

// startExitIPService serves a fixed IP over TLS with a self-signed certificate.
func startExitIPService(t *testing.T, ip string) string {
	t.Helper()
	cert := selfSignedCert(t)
	ln, err := tls.Listen("tcp", "127.0.0.1:0", &tls.Config{Certificates: []tls.Certificate{cert}})
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "text/plain")
			w.Write([]byte(ip + "\n"))
		}),
		ReadHeaderTimeout: 5 * time.Second,
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return ln.Addr().String()
}

func selfSignedCert(t *testing.T) tls.Certificate {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "mctunnel test"},
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              time.Now().Add(time.Hour),
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	// Round-trip through PEM so the certificate is parsed exactly like a real one.
	block := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	pair, err := tls.X509KeyPair(block, ecdsaPEM(t, key))
	if err != nil {
		t.Fatal(err)
	}
	return pair
}

func ecdsaPEM(t *testing.T, key *ecdsa.PrivateKey) []byte {
	t.Helper()
	der, err := x509.MarshalECPrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: der})
}

func TestCheckReportsExitIP(t *testing.T) {
	exitService := startExitIPService(t, "203.0.113.7")
	addr := startFakeSOCKS5(t, fakeSOCKS5{
		dial: func(string) (net.Conn, error) { return net.Dial("tcp", exitService) },
	})
	d, err := New(Config{Kind: SOCKS5, Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	report := Check(testContext(t), d)
	if !report.OK {
		t.Fatalf("check failed: %v", report.Err)
	}
	if report.ExitIP != "203.0.113.7" {
		t.Fatalf("exit IP = %q", report.ExitIP)
	}
	// The fake service uses a self-signed certificate, so the insecure second
	// pass must have been used and the caveat recorded.
	if !strings.Contains(report.Note, "certificate") {
		t.Fatalf("expected a certificate caveat, got %q", report.Note)
	}
	if !strings.Contains(report.String(), "203.0.113.7") {
		t.Fatalf("report string lost the IP: %q", report.String())
	}
}

func TestCheckFailsWhenProxyIsDead(t *testing.T) {
	addr := startFakeSOCKS5(t, fakeSOCKS5{
		dial: func(string) (net.Conn, error) {
			return nil, &net.OpError{Op: "dial", Err: errString("connection refused")}
		},
	})
	d, err := New(Config{Kind: SOCKS5, Addr: addr})
	if err != nil {
		t.Fatal(err)
	}
	report := Check(testContext(t), d)
	if report.OK {
		t.Fatal("expected the check to fail when the target is refused")
	}
	if report.Err == nil {
		t.Fatal("expected an error")
	}
	if !strings.HasPrefix(report.String(), "failed:") {
		t.Fatalf("report string = %q", report.String())
	}
}

func TestCheckUnreachableProxy(t *testing.T) {
	d, err := New(Config{Kind: HTTP, Addr: "127.0.0.1:1", Timeout: 2 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	report := Check(testContext(t), d)
	if report.OK || report.Err == nil {
		t.Fatalf("expected a failure, got %+v", report)
	}
}

func TestExtractIP(t *testing.T) {
	cases := map[string]string{
		"203.0.113.7\n":              "203.0.113.7",
		" 1.2.3.4 ":                  "1.2.3.4",
		"your ip is 198.51.100.9 ok": "198.51.100.9",
		"2001:db8::1":                "2001:db8::1",
		"no address here":            "",
	}
	for body, want := range cases {
		if got := extractIP(body); got != want {
			t.Errorf("extractIP(%q) = %q, want %q", body, got, want)
		}
	}
}
