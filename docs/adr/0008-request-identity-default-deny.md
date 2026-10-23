# ADR-0008: Request identity is default-deny and UUID-only

## Status

Accepted. Complements ADR-0004 (correlation is the HTTP layer's job) and ADR-0005
(`RequestID` is installed unconditionally).

## Context

`router.New` installs chi's `RealIP`, and `RequestID` accepts any inbound `X-Request-ID`.
The review measured both: a 4,096-byte ID was echoed and logged verbatim, and
`X-Forwarded-For: 203.0.113.9` became the logged `remote_addr`. chi `v5.2.1` is in the
affected range of three published spoofing advisories, and `v5.3.0` deprecates `RealIP`
in favour of client-IP middleware that never mutates `RemoteAddr`, reading the address
from context instead.

A library cannot know whether it sits behind a proxy that overwrites forwarding headers,
so trusting them by default makes every deployment's logs client-controlled. Correlation
IDs have the same problem: an unbounded, attacker-chosen string floods and poisons the
log, and is indistinguishable from a genuinely assigned ID.

## Decision

- **Upgrade chi to the current v5 line** (v5.5.0 at the time of writing) and stop using
  the deprecated `RealIP`.
- **No forwarding header is trusted by default.** The router installs no forwarding-header
  middleware unless the application configures trusted proxies; `RemoteAddr` stays the
  transport peer and is always recorded. Without trusted proxies, the resolved client IP is
  the transport peer.
- **Trust is opt-in and explicit.** When configured, use chi's client-IP middleware over
  a declared list of trusted CIDR prefixes (not a hop count), and read the address with
  `GetClientIP(ctx)`; never mutate `RemoteAddr`. The access log records the resolved
  client address separately from the transport peer.
- **`X-Request-ID` must be a UUID.** A client value is accepted only when it parses as a
  UUID and is canonicalised before use; anything else is replaced by a freshly generated
  UUID. The accepted value is echoed in the response and carried in the context, as now.
- **Deferred:** W3C `traceparent` and a provenance field distinguishing client-supplied
  from generated IDs. Both are additive and can follow without changing this posture.

## Consequences

- Default deployments get trustworthy logs: neither the correlation ID nor the client
  address can be chosen by an untrusted caller.
- Deployments behind a proxy that want the real client IP must configure trust or the log
  records the proxy's address. This is the deliberate cost of default-deny.
- Existing consumers sending non-UUID correlation IDs will see them replaced; that is the
  intended outcome, not a compatibility accident.
- `remote_addr` no longer means "client address"; the access log distinguishes
  `client_ip` (resolved, trusted) from `remote_addr` (transport peer).
