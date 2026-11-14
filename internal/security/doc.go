// Copyright 2026 The Tanpopo Authors.
// Licensed under the Apache License, Version 2.0.

// Package security implements cross-cutting security controls: three-tier AEAD AAD
// envelope encryption (vault), connect-time SSRF/DNS-rebinding dialer (egress),
// BlindHostCredentialBroker (broker), Authorized OAuth 2.1 coordinator (oauth),
// JCS hash-chained audit logs (audit), fast revocation (killswitch), and Four-Eyes.
//
// DAG Boundary (Tier 3): May import internal/pkg and internal/storage/*; must never
// import config/snapshot, higher domain packages, ingress, or tools/spikes.
package security
