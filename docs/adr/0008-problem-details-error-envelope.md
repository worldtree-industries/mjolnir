# Errors are RFC 9457 problem documents

## Status

Accepted.

## Context

`errorx` sent errors as a bespoke `{"error", "code"}` object. A 4xx echoed `err.Error()`, so a wrapped database or authentication error reached the client verbatim. A 5xx kept the handler's status line but always put `500` in the body, so a 503 told the client two different things. It could not tell whether a handler had already written part of a response, so writing and then returning an error concatenated two bodies. Its envelope also matched no standard, so clients needed mjolnir-specific parsing.

The wire format of an error is a contract with every client of every service built on mjolnir. Changing it again later breaks all of them at once.

## Decision

- **The error wire format is RFC 9457.** Every error a handler returns through `httpx` is sent as `application/problem+json` with `type`, `title`, `status`, `detail`, and `instance`. One extension member, `code`, carries a stable machine-readable identifier. `type` is `about:blank` unless the handler sets one, and `title` is `http.StatusText(status)`. `instance` is `urn:uuid:<request-id>` when the request has one (ADR-0007).
- **The detail is public for 4xx and server-only for 5xx.** A 4xx carries only the detail the handler wrote for the client. The wrapped cause is never sent. A 5xx keeps the handler's status and code, sends a fixed generic detail, and logs the handler's detail and the cause at Error. A 5xx must carry a cause, so that log line always says what failed.
- **The status and the code are never rewritten.** Masking the text is a safety property. Rewriting the status is not, so a 503 stays a 503.
- **Handlers return `error`, and a problem travels inside it.** `httpx.Handler` takes `func(http.ResponseWriter, *http.Request) error`, and `Codec.Decode` and `Codec.Respond` return `error` too: a `*Problem` on failure, untyped `nil` on success. `Handler` finds the problem with `errors.AsType[*Problem]`, so `%w` wrapping keeps it, and sends any other error as `Internal(err)`. The constructors and helpers still return `*Problem`, because callers return them straight away.
- **A started response is final.** `httpx.Handler` gives the handler a `middleware.ResponseWriter`. If the handler has sent a status or any body by the time it returns a problem, nothing more is written, and the problem is logged instead. `WriteProblem` reports this case as `ErrResponseStarted`.
- **`httpx` is the only HTTP translation module.** It owns the problem type, its writer, and the JSON codec, which bounds the request body (1 MiB by default) and answers 400, 413, and 415 as problems. `errorx` is deleted.

## Considered Options

- **Keep `{"error", "code"}` and fix the bugs.** That would fix the leaks, but it keeps a format no client library understands. The format would also need a breaking change anyway to fix the 500-in-body mismatch. Rejected.
- **Expose the 5xx detail by default, or let a handler opt in to a public 5xx detail.** Issue #20 asks for an opt-in. It is not part of this decision. The default stays masked, and an opt-in can be added later without changing the format.
- **Return `*Problem` from handlers and the codec.** That is a stronger type, but a nil `*Problem` stored in an `error` is a non-nil interface. A caller's `func() error { return codec.Decode(...) }` would then report failure on success, and `go vet` does not catch it. Rejected.
- **Detect a started response with `http.ResponseController` or by probing the writer.** net/http exposes no such query. A wrapper that records the status is the only portable signal, and the access logger already installs one. Rejected.

## Consequences

- **This is a breaking change.** Clients parsing `{"error", "code"}` must parse problem documents. Handlers move from `errorx.APIError` to returning an `error` that carries an `httpx.Problem`, and every problem carries a code. It ships in the hardening minor.
- A writer that is not a `middleware.ResponseWriter` cannot report a started response, so `WriteProblem` called directly on one writes unconditionally. `Handler` always wraps, so it does not have this gap.
- Issue #20 is only partly resolved: the status is preserved, but a 5xx cannot publish its own detail, and every 5xx still logs at Error. The health probes still write their own 503 (ADR-0005).
