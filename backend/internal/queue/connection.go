package queue

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"net"
	"os"

	"github.com/Owlah2025/gradex/backend/internal/config"
	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
)

// Connection is the single validated source for every Redis client used by
// the API and worker. It retains credentials as redacting Secret values.
type Connection struct {
	addr      string
	username  config.Secret
	password  config.Secret
	tlsConfig *tls.Config
}

func NewConnection(settings config.RedisSettings) (*Connection, error) {
	tlsConfig, err := verifiedTLSConfig(settings)
	if err != nil {
		return nil, err
	}
	return &Connection{
		addr: settings.Addr(), username: settings.Username(),
		password: settings.Password(), tlsConfig: tlsConfig,
	}, nil
}

func verifiedTLSConfig(settings config.RedisSettings) (*tls.Config, error) {
	if !settings.TLSEnabled() {
		return nil, nil
	}
	serverName := settings.TLSServerName()
	if serverName == "" {
		host, _, err := net.SplitHostPort(settings.Addr())
		if err != nil {
			return nil, errors.New("REDIS_ADDR must be host:port when Redis TLS is enabled")
		}
		serverName = host
	}
	roots, err := redisRootCAs(settings.TLSCACertFile())
	if err != nil {
		return nil, err
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, ServerName: serverName, RootCAs: roots}, nil
}

func redisRootCAs(caCertFile string) (*x509.CertPool, error) {
	roots, err := x509.SystemCertPool()
	if err != nil {
		if caCertFile == "" {
			return nil, errors.New("loading system certificate roots for Redis TLS")
		}
		roots = x509.NewCertPool()
	}
	if roots == nil {
		roots = x509.NewCertPool()
	}
	if caCertFile == "" {
		return roots, nil
	}
	pemBytes, err := os.ReadFile(caCertFile)
	if err != nil {
		return nil, fmt.Errorf("reading Redis TLS CA certificate: %w", err)
	}
	if !roots.AppendCertsFromPEM(pemBytes) {
		return nil, errors.New("REDIS_TLS_CA_CERT_FILE contains no valid certificate")
	}
	return roots, nil
}

func (c *Connection) redisOptions() *redis.Options {
	// gradex:redis-password-plaintext-boundary — credentials cross only into
	// the installed Redis drivers and remain absent from errors and telemetry.
	username := c.username.Expose()
	password := c.password.Expose()
	return &redis.Options{
		Addr: c.addr, Username: username, Password: password,
		TLSConfig: cloneTLSConfig(c.tlsConfig),
	}
}

func (c *Connection) asynqOptions() asynq.RedisClientOpt {
	options := c.redisOptions()
	return asynq.RedisClientOpt{
		Addr: options.Addr, Username: options.Username, Password: options.Password,
		TLSConfig: options.TLSConfig,
	}
}

func cloneTLSConfig(source *tls.Config) *tls.Config {
	if source == nil {
		return nil
	}
	return source.Clone()
}

func (c *Connection) NewClient() *asynq.Client { return asynq.NewClient(c.asynqOptions()) }

func (c *Connection) NewRedisClient() *redis.Client { return redis.NewClient(c.redisOptions()) }
