package cpi

import (
	"bosh-docker-cpi/config"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func testTLSIdentity(t *testing.T, name string) (string, string, tls.Certificate) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1")}}
	der, err := x509.CreateCertificate(rand.Reader, template, template, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
	keyPEM := string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}))
	identity, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		t.Fatal(err)
	}
	return certPEM, keyPEM, identity
}

func TestHTTPClientUsesOnlyConfiguredCA(t *testing.T) {
	ca, key, identity := testTLSIdentity(t, "configured private authority")
	_, _, otherIdentity := testTLSIdentity(t, "unrelated authority")
	client, err := (Factory{}).httpClient(config.DockerOpts{Host: "tcp://127.0.0.1:2376", TLS: config.DockerOptsTLS{CA: ca, Certificate: ca, PrivateKey: key}})
	if err != nil {
		t.Fatal(err)
	}
	expectedRoots := x509.NewCertPool()
	expectedRoots.AppendCertsFromPEM([]byte(ca))
	if !client.Transport.(*http.Transport).TLSClientConfig.RootCAs.Equal(expectedRoots) {
		t.Fatal("trust roots differ from the configured authority")
	}
	for _, tc := range []struct {
		name     string
		identity tls.Certificate
		wantOK   bool
	}{{"configured", identity, true}, {"unrelated", otherIdentity, false}} {
		t.Run(tc.name, func(t *testing.T) {
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(http.StatusNoContent) }))
			server.TLS = &tls.Config{Certificates: []tls.Certificate{tc.identity}}
			server.StartTLS()
			defer server.Close()
			response, err := client.Get(server.URL)
			if tc.wantOK && err != nil {
				t.Fatal(err)
			}
			if !tc.wantOK && err == nil {
				t.Fatal("accepted unrelated server authority")
			}
			if response != nil {
				if _, err := io.Copy(io.Discard, response.Body); err != nil {
					t.Fatal(err)
				}
				if err := response.Body.Close(); err != nil {
					t.Fatal(err)
				}
			}
		})
	}
}

func TestHTTPClientRejectsInvalidCA(t *testing.T) {
	client, err := (Factory{}).httpClient(config.DockerOpts{Host: "tcp://daemon:2376", TLS: config.DockerOptsTLS{CA: "invalid"}})
	if err == nil || client != nil {
		t.Fatalf("invalid CA must return an error, got client %v error %v", client, err)
	}
}
