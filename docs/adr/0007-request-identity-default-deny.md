# Request identity is default-deny

## Status

Accepted.

## Context

`RequestID` accepted any inbound `X-Request-ID` and echoed it back. A probe echoed 4,096 bytes. The value goes into the response header, the request context, and every request log line, so an attacker who chooses it can poison correlation, forge log entries that look like they belong to another request, and inflate the logs. `net/http` already refuses CR/LF in header values, so response splitting is not the risk. The risks are an unbounded value and one the attacker controls.

The router also installed chi's `RealIP`. It rewrites `RemoteAddr` from `X-Forwarded-For`, `X-Real-IP`, or `True-Client-IP` whether or not a proxy in front set them. chi v5.3.0 fixed three spoofing advisories (GHSA-3fxj-6jh8-hvhx, GHSA-rjr7-jggh-pgcp, GHSA-9g5q-2w5x-hmxf) and deprecated `RealIP`. It also added `ClientIPFrom*` middleware that trusts forwarding headers only from configured proxies.

Both inputs are client-supplied claims about who the request is. The library cannot know which proxies a deployment runs, so the only safe default is to trust none of them.

## Decision

- **Request identity is default-deny.** The library trusts no client-supplied identity value unless the value is structurally valid, or the application has configured where it may come from.
- **`X-Request-ID` is accepted only when it is a UUID.** The middleware checks the length first. Anything longer than the longest spelling `uuid.Parse` accepts (`urn:uuid:` plus 36 characters, 45 bytes) is never parsed. A value that parses is canonicalised with `uuid.Parse(v).String()`. Braces, `urn:uuid:`, upper case, and bare 32-hex all become the lowercase hyphenated form. Every other value, including an empty one, is replaced by a fresh v4 UUID and is never echoed. `GetRequestID` therefore returns a canonical UUID, or `""` when the middleware did not run.
- **No forwarding header is trusted unless trusted CIDR prefixes are configured.** With no prefixes, `X-Forwarded-For`, `X-Real-IP`, and `True-Client-IP` do not change the resolved client address, and `remote_addr` stays the transport peer. With prefixes, resolution goes through chi's trusted-proxy `ClientIPFrom*` middleware. chi is pinned at v5.5.0.

The request-ID half ships with issue #31. The client-address half needs router configuration and ships with issue #33. Until then the router keeps the deprecated `RealIP` behind a targeted lint suppression that names #33.

## Considered Options

- **Accept any value up to a length limit.** This bounds the size but still lets an attacker choose the content, so they can collide with or impersonate another request's id. Rejected.
- **Accept any printable token matching a character-class regex.** This is the same collision problem, and it adds a grammar the library would have to own. Rejected. UUIDs are what the middleware generates, and callers that propagate ids almost always forward them.
- **Echo the raw spelling when it parses.** The same request id would appear in several spellings across log lines and break exact-match correlation. Rejected in favour of canonicalisation.
- **Trust forwarding headers by default, as `RealIP` did.** That makes any client able to choose its logged address. Rejected.

## Consequences

- **This is a breaking change in behaviour.** A caller that propagates non-UUID ids, such as `req-abc-123`, now sees them replaced, and its ids no longer join across services. Such callers must switch to UUIDs. It is released as part of the hardening minor.
- The nil UUID and non-v4 UUIDs are accepted because they parse. Only generated ids are guaranteed to be v4.
- A deployment behind a proxy loses the original client address in logs until it configures trusted prefixes (issue #33). This is deliberate. A missing address is safer than a forged one.
