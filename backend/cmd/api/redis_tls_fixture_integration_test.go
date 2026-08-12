//go:build integration

package main

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/redis/go-redis/v9"
)

type tlsRedisFixture struct {
	addr     string
	password string
	caFile   string
	name     string
}

func newTLSRedisFixture(t *testing.T) tlsRedisFixture {
	t.Helper()
	dir, err := os.MkdirTemp("/var/tmp", "gradex-t108-redis-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	password := randomFixtureSecret(t)
	writeRedisCertificates(t, dir)
	name := "gradex-t108-" + randomFixtureSecret(t)[:12]
	command := `exec redis-server --appendonly no --save "" --port 0 --tls-port 6379 --tls-cert-file /tls/server.crt --tls-key-file /tls/server.key --tls-ca-cert-file /tls/ca.crt --tls-auth-clients no --requirepass "$REDIS_PASSWORD"`
	runFixtureCommand(t, "docker", "run", "-d", "--name", name, "--user", "0", "-p", "127.0.0.1::6379", "-e", "REDIS_PASSWORD="+password, "-v", dir+":/tls:ro", "--entrypoint", "/bin/sh", "redis:7.4.9-alpine", "-ec", command)
	t.Cleanup(func() { _ = exec.Command("docker", "rm", "-f", name).Run() })
	portOutput, portErr := exec.Command("docker", "port", name, "6379/tcp").CombinedOutput()
	if portErr != nil {
		logs, _ := exec.Command("docker", "logs", name).CombinedOutput()
		t.Fatalf("TLS Redis did not publish its TLS port: %v: %s", portErr, logs)
	}
	port := strings.TrimSpace(string(portOutput))
	_, portNumber, err := net.SplitHostPort(port)
	if err != nil {
		t.Fatalf("reading TLS Redis host port: %v", err)
	}
	fixture := tlsRedisFixture{addr: net.JoinHostPort("localhost", portNumber), password: password, caFile: filepath.Join(dir, "ca.crt"), name: name}
	fixture.waitReady(t)
	return fixture
}

func (f tlsRedisFixture) waitReady(t *testing.T) {
	t.Helper()
	pem, err := os.ReadFile(f.caFile)
	if err != nil {
		t.Fatalf("reading Redis test CA: %v", err)
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(pem) {
		t.Fatal("loading Redis test CA")
	}
	client := redis.NewClient(&redis.Options{Addr: f.addr, Password: f.password, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "localhost", RootCAs: pool}})
	defer client.Close()
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		err = client.Ping(ctx).Err()
		cancel()
		if err == nil {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatalf("authenticated verified TLS Redis did not become ready: %v", err)
}

func writeRedisCertificates(t *testing.T, dir string) {
	t.Helper()
	runFixtureCommand(t, "openssl", "req", "-x509", "-newkey", "rsa:2048", "-nodes", "-keyout", filepath.Join(dir, "ca.key"), "-out", filepath.Join(dir, "ca.crt"), "-subj", "/CN=gradex-t108-test-ca", "-days", "1")
	runFixtureCommand(t, "openssl", "req", "-newkey", "rsa:2048", "-nodes", "-keyout", filepath.Join(dir, "server.key"), "-out", filepath.Join(dir, "server.csr"), "-subj", "/CN=localhost", "-addext", "subjectAltName=DNS:localhost")
	runFixtureCommand(t, "openssl", "x509", "-req", "-in", filepath.Join(dir, "server.csr"), "-CA", filepath.Join(dir, "ca.crt"), "-CAkey", filepath.Join(dir, "ca.key"), "-CAcreateserial", "-out", filepath.Join(dir, "server.crt"), "-days", "1", "-copy_extensions", "copy")
	for _, file := range []string{"ca.key", "server.key"} {
		if err := os.Chmod(filepath.Join(dir, file), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func randomFixtureSecret(t *testing.T) string {
	t.Helper()
	bytes := make([]byte, 24)
	if _, err := rand.Read(bytes); err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(bytes)
}

func runFixtureCommand(t *testing.T, name string, args ...string) string {
	t.Helper()
	output, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("%s fixture command failed: %v: %s", name, err, output)
	}
	return string(output)
}

func TestTLSRedisFixtureUsesVerifiedTLSAndAuthentication(t *testing.T) {
	fixture := newTLSRedisFixture(t)
	if fixture.addr == "" || fixture.caFile == "" || fixture.password == "" {
		t.Fatal("TLS Redis fixture is incomplete")
	}
	wrong := redis.NewClient(&redis.Options{Addr: fixture.addr, Password: "wrong", TLSConfig: &tls.Config{MinVersion: tls.VersionTLS12, ServerName: "localhost"}})
	defer wrong.Close()
	if err := wrong.Ping(t.Context()).Err(); err == nil {
		t.Fatal("untrusted Redis certificate was accepted")
	}
}
