---
title: Error reference
sidebar_position: 2
---

# Error reference

On error the envelope's `code` is the canonical `google.rpc.Code` integer and `data.error` carries
structured `ErrorInfo`:

```jsonc
{ "code": 9, "message": "All units of 'Voucher 100K' have been awarded.", "trace_id": "01HX3…",
  "data": { "error": {
    "status": "FAILED_PRECONDITION",      // canonical code name
    "reason": "PRIZE_OUT_OF_STOCK",       // STABLE machine-readable reason — never renamed
    "domain": "muse.game",
    "metadata": { "prize_id": "prize_001" }
  } } }
```

- **Branch on `reason`**, not on `message` (human-readable, may change) or HTTP status.
- Field validation adds `data.error.field_violations[]`.
- `trace_id` is echoed in the `X-Trace-Id` header and correlates to server logs/metrics.

| `reason` | gRPC status (code) | HTTP | Meaning |
|---|---|---|---|
| `VALIDATION_FAILED` | INVALID_ARGUMENT (3) | 400 | Request/config failed validation (see `field_violations`). |
| `CHEAT_DETECTED` | INVALID_ARGUMENT (3) | 400 | Anti-cheat rejected the play. |
| `UNAUTHENTICATED` | UNAUTHENTICATED (16) | 401 | Missing/invalid token. |
| `PERMISSION_DENIED` | PERMISSION_DENIED (7) | 403 | Authenticated but lacks the required role. |
| `RESOURCE_NOT_FOUND` | NOT_FOUND (5) | 404 | Entity does not exist in scope. |
| `SESSION_EXPIRED` | FAILED_PRECONDITION (9) | 400 | Session TTL elapsed before `play`. |
| `SESSION_CONSUMED` | FAILED_PRECONDITION (9) | 400 | Single-use session already played. |
| `SESSION_INVALID` | FAILED_PRECONDITION (9) | 400 | Session unknown / not bound to this player+game. |
| `OUT_OF_TURNS` | FAILED_PRECONDITION (9) | 400 | No remaining plays. |
| `GAME_NOT_ACTIVE` | FAILED_PRECONDITION (9) | 400 | Draft/paused/ended or outside window. |
| `REWARD_INVALID_STATE` | FAILED_PRECONDITION (9) | 400 | Illegal reward lifecycle transition. |
| `TASK_INVALID_STATE` | FAILED_PRECONDITION (9) | 400 | Illegal fulfillment-task transition. |
| `PRIZE_OUT_OF_STOCK` | ABORTED (10) | 409 | All units awarded (atomic stock check failed). |
| `ALREADY_EXISTS` | ALREADY_EXISTS (6) | 409 | Duplicate create, or a once-only action repeated. |
| `REWARD_ALREADY_CLAIMED` | ALREADY_EXISTS (6) | 409 | Reward already claimed/fulfilled. |
| `CONTACT_CONFLICT` | ALREADY_EXISTS (6) | 409 | Verified contact maps to a different identity. |
| `RATE_LIMITED` | RESOURCE_EXHAUSTED (8) | 429 | Rate limit exceeded; honor `Retry-After`. |
| `HANDLER_NOT_FOUND` | INTERNAL (13) | 500 | Config references an unregistered handler/seed/validator. |
| `INTERNAL` | INTERNAL (13) | 500 | Unexpected server error. |

Reasons are defined in `gamekit/gkerr` and mapped to gRPC codes in `pkg/apierr`; the BFF transcodes
the gRPC status to HTTP. Unlisted reasons default to `INTERNAL` (500). This table mirrors
`docs/ERRORS.md` in the repo.
