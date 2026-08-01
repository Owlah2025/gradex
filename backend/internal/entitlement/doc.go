// Package entitlement evaluates existing Entitlements for protected learning
// operations.
//
// S4 is deliberately a consumer of the grant record: it reads scope,
// effective expiry, revocation, suspension, and retirement eligibility. S6 is
// the sole production owner of Entitlement creation. This package therefore
// exposes no production grant-creation command or write repository.
//
// Course Access Invitations are provenance recorded on an Entitlement. They
// are never a live authorization dependency and this package imports no S6
// invitation service. HTTP handlers and delivery services must use Evaluator
// rather than duplicating expiry, scope, suspension, or retirement checks.
package entitlement
