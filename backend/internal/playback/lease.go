// Package playback owns the one protected-video playback lease a Student
// account may hold at a time.
//
// The lease is the concurrency control that makes account sharing
// inconvenient: two browsers may both stay logged in on their own trusted
// devices, and only one of them may hold protected playback authority. It is
// deliberately *not* a session control. Nothing here authenticates anyone,
// nothing here grants entitlement, and losing a lease never ends a session.
//
// State lives in Redis rather than in PostgreSQL or in process memory. Process
// memory is disqualified outright — the API runs as several instances and a
// per-process lock would grant one lease per instance, which is the exact
// failure this feature exists to prevent. Redis is chosen over a database row
// because the lease is ephemeral, rewritten every heartbeat, and must expire on
// its own when a laptop sleeps: a TTL is the correct primitive for that, and a
// row with an expires_at column would need a sweeper to do worse.
package playback

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/redis/go-redis/v9"
)

// Outcomes of an attempted lease operation.
var (
	// ErrHeldByAnotherDevice means a live lease belongs to a different trusted
	// device of the same account. This is the account-sharing refusal.
	ErrHeldByAnotherDevice = errors.New("protected playback is active on another device")

	// ErrLeaseNotHeld means the exact lease presented no longer owns the key:
	// it expired, or a newer playback instance replaced it.
	//
	// Distinct from ErrHeldByAnotherDevice because the remedy differs. A stale
	// lease must never resurrect itself — the caller has to acquire a new one
	// through the authorization path, which re-checks entitlement and device
	// trust on the way.
	ErrLeaseNotHeld = errors.New("playback lease is no longer held")

	// ErrCoordinatorUnavailable means authoritative state could not be
	// established. Callers must fail closed on it.
	ErrCoordinatorUnavailable = errors.New("playback coordination is unavailable")
)

// Lease is one playback instance's authority.
//
// LeaseID is the field that makes the whole thing safe against its own history.
// Keying only on account and device would let a forgotten tab on the same
// device renew — or worse, release — the lease belonging to the video the
// Student is actually watching. Every operation therefore compares the exact
// lease identity, and a lease that has been replaced fails every one of them.
type Lease struct {
	LeaseID   string
	AccountID string
	DeviceID  string
	SessionID string
	LessonID  string

	IssuedAt        time.Time
	LastHeartbeatAt time.Time
	ExpiresAt       time.Time
}

// Acquisition reports what an acquire actually did.
type Acquisition struct {
	Lease Lease
	// ReplacedSameDevice is true when this acquire took the lease from a
	// previous playback instance on the *same* trusted device — a second tab,
	// or the same tab starting a different lesson. It is not a conflict and the
	// Student is never shown an "another device" message for it; the previous
	// instance simply discovers on its next heartbeat that it is stale.
	ReplacedSameDevice bool
	ReplacedLeaseID    string
}

// Settings are the tunable lease timings.
type Settings struct {
	// TTL is how long a lease survives without a heartbeat. It bounds how long
	// a crashed tab, a slept laptop, or a dropped connection can hold the
	// account's playback slot hostage.
	TTL time.Duration
	// HeartbeatInterval is what the first-party player is told to use. It is
	// published to the client rather than assumed by it, so the two values can
	// never drift apart across a deployment.
	HeartbeatInterval time.Duration
}

// Validate refuses timings that cannot work rather than accepting them and
// producing playback that dies mid-video. The TTL must leave room for at least
// two missed heartbeats, or ordinary jitter on a mobile network would look
// exactly like a closed tab.
func (s Settings) Validate() error {
	if s.HeartbeatInterval <= 0 {
		return errors.New("playback heartbeat interval must be positive")
	}
	if s.TTL <= 0 {
		return errors.New("playback lease TTL must be positive")
	}
	if s.TTL < 2*s.HeartbeatInterval {
		return errors.New("playback lease TTL must allow at least two heartbeat intervals")
	}
	if s.TTL > 10*time.Minute {
		return errors.New("playback lease TTL above ten minutes would strand the account's slot")
	}
	return nil
}

// The scripts are compiled once at package initialization. go-redis caches the
// SHA and uses EVALSHA after the first call, so the script body crosses the
// wire once per Redis process rather than once per playback request.
var (
	acquire       = redis.NewScript(acquireScript)
	renew         = redis.NewScript(renewScript)
	validate      = redis.NewScript(validateScript)
	release       = redis.NewScript(releaseScript)
	releaseDevice = redis.NewScript(revokeDeviceScript)
)

const keyPrefix = "gradex:playback:lease:"

func leaseKey(accountID string) string { return keyPrefix + accountID }

// Redis client surface. redis.Scripter is exactly the capability this package
// needs, and taking the library's own interface means a *redis.Client, a
// cluster client, and a second API instance's client are all equally usable —
// which is what the multi-instance tests drive.
type scriptRunner = redis.Scripter

// Coordinator is the cross-instance playback authority.
type Coordinator struct {
	client   scriptRunner
	settings Settings
}

func NewCoordinator(client scriptRunner, settings Settings) (*Coordinator, error) {
	if client == nil {
		return nil, errors.New("playback coordinator requires a Redis client")
	}
	if err := settings.Validate(); err != nil {
		return nil, err
	}
	return &Coordinator{client: client, settings: settings}, nil
}

func (c *Coordinator) Settings() Settings { return c.settings }

// acquireScript is the whole concurrency decision, executed as one Redis
// operation.
//
// Doing it in Lua rather than as WATCH/GET/SET or SETNX-then-inspect is the
// point: two API instances handling two simultaneous "start playback" requests
// for the same account must produce exactly one winner, and a check-then-set
// across a network round trip cannot promise that. Redis executes this script
// atomically, so the losing instance observes the winner's write, never the
// gap before it.
//
// The clock is Redis TIME, not the caller's wall clock. Instances disagree
// about the time by seconds; the lease must not.
const acquireScript = `
local key = KEYS[1]
local account = ARGV[1]
local device = ARGV[2]
local lease = ARGV[3]
local session = ARGV[4]
local lesson = ARGV[5]
local ttl = tonumber(ARGV[6])

local clock = redis.call("TIME")
local now = (tonumber(clock[1]) * 1000) + math.floor(tonumber(clock[2]) / 1000)

local held = redis.call("HMGET", key, "device_id", "lease_id")
local held_device = held[1]
local held_lease = held[2]

local replaced = ""
if held_device then
  if held_device ~= device then
    -- Another trusted device of this same account is watching. The caller is
    -- told only that; nothing about the other device leaves this script.
    return {"CONFLICT", "", "", 0}
  end
  -- Same device, different playback instance. Replacing is correct: one
  -- protected video per account means one per device too, and the Student
  -- opening a second lesson expects the new one to play.
  replaced = held_lease or ""
end

redis.call("HSET", key,
  "account_id", account,
  "device_id", device,
  "lease_id", lease,
  "session_id", session,
  "lesson_id", lesson,
  "issued_at", now,
  "heartbeat_at", now)
redis.call("PEXPIRE", key, ttl)

if replaced ~= "" then
  return {"REPLACED", tostring(now), replaced, ttl}
end
return {"ACQUIRED", tostring(now), "", ttl}
`

// renewScript extends only the exact lease that still owns the key.
//
// A superseded lease id fails here rather than re-creating state. That is what
// stops an old tab from resurrecting a lease the account has legitimately moved
// on from, and it is why renew is a separate operation from acquire instead of
// an acquire that happens to succeed.
const renewScript = `
local key = KEYS[1]
local device = ARGV[1]
local lease = ARGV[2]
local ttl = tonumber(ARGV[3])

local held = redis.call("HMGET", key, "device_id", "lease_id")
if not held[1] then
  return {"GONE", "0"}
end
if held[1] ~= device or held[2] ~= lease then
  return {"SUPERSEDED", "0"}
end

local clock = redis.call("TIME")
local now = (tonumber(clock[1]) * 1000) + math.floor(tonumber(clock[2]) / 1000)
redis.call("HSET", key, "heartbeat_at", now)
redis.call("PEXPIRE", key, ttl)
return {"RENEWED", tostring(now)}
`

// validateScript is a pure read. It deliberately does not extend the TTL:
// fetching a manifest proves authority existed at that instant, not that
// anybody is still watching, and letting a read refresh the lease would let a
// script that polls manifests hold the account's slot forever without ever
// playing a frame.
const validateScript = `
local key = KEYS[1]
local device = ARGV[1]
local lease = ARGV[2]

local held = redis.call("HMGET", key, "device_id", "lease_id")
if not held[1] then
  return {"GONE"}
end
if held[1] ~= device then
  return {"CONFLICT"}
end
if held[2] ~= lease then
  return {"SUPERSEDED"}
end
return {"VALID"}
`

// releaseScript deletes only when the exact holder asks. A stale lease id
// releasing the key would let a closed tab hand the account's playback slot
// away from the video that is actually running.
const releaseScript = `
local key = KEYS[1]
local device = ARGV[1]
local lease = ARGV[2]

local held = redis.call("HMGET", key, "device_id", "lease_id")
if not held[1] then
  return {"GONE"}
end
if held[1] ~= device or held[2] ~= lease then
  return {"SUPERSEDED"}
end
redis.call("DEL", key)
return {"RELEASED"}
`

// revokeDeviceScript drops the lease if and only if the named device holds it.
//
// Used when a trusted device is revoked. The condition matters: revoking one
// device must not stop the Student's *other* device mid-lesson, and an
// unconditional DEL on the account key would do exactly that.
const revokeDeviceScript = `
local key = KEYS[1]
local device = ARGV[1]
local held = redis.call("HGET", key, "device_id")
if not held then
  return {"GONE"}
end
if held ~= device then
  return {"OTHER_DEVICE"}
end
redis.call("DEL", key)
return {"RELEASED"}
`

// Acquire mints a new playback instance for this device.
//
// The lease id is supplied by the caller rather than generated here so that the
// same value can be bound into the signed playback authorization in the same
// step. A lease whose id nobody holds is useless, and one that exists without a
// matching authorization would be worse.
func (c *Coordinator) Acquire(ctx context.Context, lease Lease) (Acquisition, error) {
	if err := requireIdentity(lease); err != nil {
		return Acquisition{}, err
	}
	values, err := c.run(ctx, acquire, lease.AccountID,
		lease.AccountID, lease.DeviceID, lease.LeaseID, lease.SessionID, lease.LessonID,
		c.settings.TTL.Milliseconds(),
	)
	if err != nil {
		return Acquisition{}, err
	}
	switch status(values) {
	case "CONFLICT":
		return Acquisition{}, ErrHeldByAnotherDevice
	case "ACQUIRED", "REPLACED":
		issued := millisAt(values, 1)
		granted := lease
		granted.IssuedAt = issued
		granted.LastHeartbeatAt = issued
		granted.ExpiresAt = issued.Add(c.settings.TTL)
		return Acquisition{
			Lease:              granted,
			ReplacedSameDevice: status(values) == "REPLACED",
			ReplacedLeaseID:    stringAt(values, 2),
		}, nil
	}
	return Acquisition{}, fmt.Errorf("%w: unexpected acquire status", ErrCoordinatorUnavailable)
}

// Renew is the heartbeat.
func (c *Coordinator) Renew(ctx context.Context, accountID, deviceID, leaseID string) (time.Time, error) {
	if accountID == "" || deviceID == "" || leaseID == "" {
		return time.Time{}, fmt.Errorf("%w: incomplete lease identity", ErrLeaseNotHeld)
	}
	values, err := c.run(ctx, renew, accountID, deviceID, leaseID, c.settings.TTL.Milliseconds())
	if err != nil {
		return time.Time{}, err
	}
	switch status(values) {
	case "RENEWED":
		return millisAt(values, 1).Add(c.settings.TTL), nil
	case "GONE", "SUPERSEDED":
		return time.Time{}, ErrLeaseNotHeld
	}
	return time.Time{}, fmt.Errorf("%w: unexpected renew status", ErrCoordinatorUnavailable)
}

// Validate answers whether this exact lease still owns the account's playback.
func (c *Coordinator) Validate(ctx context.Context, accountID, deviceID, leaseID string) error {
	if accountID == "" || deviceID == "" || leaseID == "" {
		return fmt.Errorf("%w: incomplete lease identity", ErrLeaseNotHeld)
	}
	values, err := c.run(ctx, validate, accountID, deviceID, leaseID)
	if err != nil {
		return err
	}
	switch status(values) {
	case "VALID":
		return nil
	case "CONFLICT":
		return ErrHeldByAnotherDevice
	case "GONE", "SUPERSEDED":
		return ErrLeaseNotHeld
	}
	return fmt.Errorf("%w: unexpected validate status", ErrCoordinatorUnavailable)
}

// Release is the cooperative stop: the player leaving a lesson hands the slot
// back instead of making the other device wait out the TTL.
func (c *Coordinator) Release(ctx context.Context, accountID, deviceID, leaseID string) error {
	if accountID == "" || deviceID == "" || leaseID == "" {
		return fmt.Errorf("%w: incomplete lease identity", ErrLeaseNotHeld)
	}
	values, err := c.run(ctx, release, accountID, deviceID, leaseID)
	if err != nil {
		return err
	}
	switch status(values) {
	case "RELEASED":
		return nil
	case "GONE", "SUPERSEDED":
		// Already gone, or already replaced by a newer instance. Both mean this
		// caller holds nothing, and neither is an error worth surfacing: the
		// slot is not theirs to free.
		return nil
	}
	return fmt.Errorf("%w: unexpected release status", ErrCoordinatorUnavailable)
}

// ReleaseDevice drops the account's lease only when the named device holds it.
func (c *Coordinator) ReleaseDevice(ctx context.Context, accountID, deviceID string) error {
	if accountID == "" || deviceID == "" {
		return fmt.Errorf("%w: incomplete device identity", ErrLeaseNotHeld)
	}
	values, err := c.run(ctx, releaseDevice, accountID, deviceID)
	if err != nil {
		return err
	}
	switch status(values) {
	case "RELEASED", "GONE", "OTHER_DEVICE":
		return nil
	}
	return fmt.Errorf("%w: unexpected device release status", ErrCoordinatorUnavailable)
}

func requireIdentity(lease Lease) error {
	if lease.AccountID == "" || lease.DeviceID == "" || lease.LeaseID == "" {
		return fmt.Errorf("%w: incomplete lease identity", ErrCoordinatorUnavailable)
	}
	return nil
}

// run executes one script and normalizes every transport failure into
// ErrCoordinatorUnavailable.
//
// Every caller of this package fails closed on that error. There is no local
// fallback path and there is deliberately no "allow playback if Redis is down"
// branch: an outage that silently disabled the concurrency control would be
// indistinguishable, from the outside, from the feature not existing.
func (c *Coordinator) run(ctx context.Context, script *redis.Script, accountID string, args ...any) ([]any, error) {
	result, err := script.Run(ctx, c.client, []string{leaseKey(accountID)}, args...).Result()
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrCoordinatorUnavailable, err)
	}
	values, ok := result.([]any)
	if !ok || len(values) == 0 {
		return nil, fmt.Errorf("%w: malformed coordination reply", ErrCoordinatorUnavailable)
	}
	return values, nil
}

func status(values []any) string { return stringAt(values, 0) }

func stringAt(values []any, index int) string {
	if index >= len(values) {
		return ""
	}
	switch value := values[index].(type) {
	case string:
		return value
	case []byte:
		return string(value)
	}
	return ""
}

func millisAt(values []any, index int) time.Time {
	raw := stringAt(values, index)
	millis, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || millis <= 0 {
		return time.Time{}
	}
	return time.UnixMilli(millis).UTC()
}
