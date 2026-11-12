//go:build integration

package integration_test

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"github.com/scalepilot/scalepilot-operator/internal/queue"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/wait"
	"math/big"
	"testing"
	"time"
)

func TestAuthenticatedMutualTLSRedis(t *testing.T) {
	ca, cert, key := testCertificates(t)
	ctx := context.Background()
	request := testcontainers.ContainerRequest{Image: "redis:7.4.7-alpine", ExposedPorts: []string{"6379/tcp"}, Cmd: []string{"redis-server", "--port", "0", "--tls-port", "6379", "--tls-cert-file", "/tmp/server.crt", "--tls-key-file", "/tmp/server.key", "--tls-ca-cert-file", "/tmp/ca.crt", "--tls-auth-clients", "yes", "--requirepass", "only-for-integration-tests"}, WaitingFor: wait.ForLog("Ready to accept connections")}
	for path, data := range map[string][]byte{"/tmp/ca.crt": ca, "/tmp/server.crt": cert, "/tmp/server.key": key} {
		request.Files = append(request.Files, testcontainers.ContainerFile{Reader: bytes.NewReader(data), ContainerFilePath: path, FileMode: 0644})
	}
	container, e := testcontainers.GenericContainer(ctx, testcontainers.GenericContainerRequest{ContainerRequest: request, Started: true})
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		if e := container.Terminate(ctx); e != nil {
			t.Error(e)
		}
	})
	host, e := container.Host(ctx)
	if e != nil {
		t.Fatal(e)
	}
	port, e := container.MappedPort(ctx, "6379/tcp")
	if e != nil {
		t.Fatal(e)
	}
	data := map[string][]byte{"address": []byte(fmt.Sprintf("%s:%s", host, port.Port())), "password": []byte("only-for-integration-tests"), "ca.crt": ca, "tls.crt": cert, "tls.key": key}
	client, e := queue.NewClient(data, true, "redis.test")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = client.Close() }()
	q := queue.Queue{Client: client, Name: "secure", Group: "workers"}
	op, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if e = q.Ensure(op); e != nil {
		t.Fatal(e)
	}
	if _, e = q.Snapshot(op); e != nil {
		t.Fatal(e)
	}
	badName, e := queue.NewClient(data, true, "wrong.test")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = badName.Close() }()
	badCtx, badCancel := context.WithTimeout(ctx, 2*time.Second)
	defer badCancel()
	if e = badName.Ping(badCtx).Err(); e == nil {
		t.Fatal("server certificate name was not verified")
	}
	data["password"] = []byte("wrong-password")
	badAuth, e := queue.NewClient(data, true, "redis.test")
	if e != nil {
		t.Fatal(e)
	}
	defer func() { _ = badAuth.Close() }()
	if e = badAuth.Ping(op).Err(); e == nil {
		t.Fatal("Redis authentication was not enforced")
	}
	data["ca.crt"] = []byte("not a CA")
	if _, e = queue.NewClient(data, true, "redis.test"); e == nil {
		t.Fatal("malformed CA accepted")
	}
}

func testCertificates(t *testing.T) ([]byte, []byte, []byte) {
	t.Helper()
	caKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now()
	caTemplate := &x509.Certificate{SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "ephemeral integration CA"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature}
	caDER, e := x509.CreateCertificate(rand.Reader, caTemplate, caTemplate, &caKey.PublicKey, caKey)
	if e != nil {
		t.Fatal(e)
	}
	leafKey, e := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if e != nil {
		t.Fatal(e)
	}
	leaf := &x509.Certificate{SerialNumber: big.NewInt(2), Subject: pkix.Name{CommonName: "redis.test"}, DNSNames: []string{"redis.test"}, NotBefore: now.Add(-time.Minute), NotAfter: now.Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth}}
	certDER, e := x509.CreateCertificate(rand.Reader, leaf, caTemplate, &leafKey.PublicKey, caKey)
	if e != nil {
		t.Fatal(e)
	}
	keyDER, e := x509.MarshalPKCS8PrivateKey(leafKey)
	if e != nil {
		t.Fatal(e)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: caDER}), pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certDER}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}
