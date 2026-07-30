// Package catalogpublic provides the public, read-only catalogue boundary.
//
// It reads S2 catalogue tables but creates no write path and makes no
// entitlement or authority decision. S4 owns protected delivery and
// entitlement evaluation; S5 owns progress; S6 owns invitations and
// enrollment behaviour.
package catalogpublic
