# Error reference

Every REST response uses the uniform envelope `{ code, message, trace_id, data }`. On error,
`code` is the canonical [`google.rpc.Code`](https://github.com/googleapis/googleapis/blob/master/google/rpc/code.proto)
integer and `data.error` carries structured `ErrorInfo`:

```jsonc
{ "code": 9, "message": "All units of 'Voucher 100K' have been awarded.", "trace_id": "01HX3…",
  "data": { "error": {
    "status": "FAILED_PRECONDITION",      // canonical code name
    "reason": "PRIZE_OUT_OF_STOCK",       // STABLE machine-readable reason — never renamed
    "domain": "muse.game",
    "metadata": { "prize_id": "prize_001" }
  } } }
```

- **`reason`** is the contract for programmatic handling — it is stable (UPPER_SNAKE_CASE) and never
  renamed. Branch on `reason`, not on `message` (human-readable, may change) or HTTP status.
- Field validation adds `data.error.field_violations[]` (mirrors `google.rpc.BadRequest`).
- `trace_id` is echoed in the `X-Trace-Id` header and correlates to the server trace/logs.

The mapping below is the single source of truth in code: reasons are defined in
[`gamekit/gkerr`](../gamekit/gkerr/gkerr.go) and mapped to gRPC codes in
[`pkg/apierr`](../pkg/apierr/apierr.go); the BFF transcodes the gRPC status to HTTP.

| `reason` | gRPC status (code) | HTTP | Meaning |
|---|---|---|---|
| `VALIDATION_FAILED` | INVALID_ARGUMENT (3) | 400 | Request/config failed validation (see `field_violations`). |
| `CHEAT_DETECTED` | INVALID_ARGUMENT (3) | 400 | Anti-cheat rejected the play (bad replay, over-catch, score ceiling, unknown drop). |
| `UNAUTHENTICATED` | UNAUTHENTICATED (16) | 401 | Missing/invalid player or admin token. |
| `PERMISSION_DENIED` | PERMISSION_DENIED (7) | 403 | Authenticated but lacks the required role (admin RBAC). |
| `RESOURCE_NOT_FOUND` | NOT_FOUND (5) | 404 | Game/prize/quest/leaderboard/integration id does not exist in scope. |
| `SESSION_EXPIRED` | FAILED_PRECONDITION (9) | 400 | The play session TTL elapsed before `play`. |
| `SESSION_CONSUMED` | FAILED_PRECONDITION (9) | 400 | The single-use session was already played. |
| `SESSION_INVALID` | FAILED_PRECONDITION (9) | 400 | Session unknown or not bound to this player/game. |
| `OUT_OF_TURNS` | FAILED_PRECONDITION (9) | 400 | No remaining plays (per-user/day cap or no granted turns). |
| `GAME_NOT_ACTIVE` | FAILED_PRECONDITION (9) | 400 | Game/quest is draft/paused/ended or outside its window. |
| `REWARD_INVALID_STATE` | FAILED_PRECONDITION (9) | 400 | Reward lifecycle transition not allowed from its current state. |
| `TASK_INVALID_STATE` | FAILED_PRECONDITION (9) | 400 | Fulfillment task cannot be retried/transitioned from its current state. |
| `PRIZE_OUT_OF_STOCK` | ABORTED (10) | 409 | All units of the prize have been awarded (atomic stock check failed). |
| `ALREADY_EXISTS` | ALREADY_EXISTS (6) | 409 | Duplicate create, or a once-only action repeated (e.g. milestone already granted). |
| `REWARD_ALREADY_CLAIMED` | ALREADY_EXISTS (6) | 409 | Reward already claimed/fulfilled. |
| `CONTACT_CONFLICT` | ALREADY_EXISTS (6) | 409 | A verified contact already maps to a different identity. |
| `RATE_LIMITED` | RESOURCE_EXHAUSTED (8) | 429 | Per-player/IP rate limit exceeded; honor the `Retry-After` header. |
| `HANDLER_NOT_FOUND` | INTERNAL (13) | 500 | Game config references an unregistered handler/seed/validator (misconfiguration). |
| `INTERNAL` | INTERNAL (13) | 500 | Unexpected server error. |

Reasons not listed default to `INTERNAL` (500). New reasons must be added to `gkerr`, mapped in
`apierr`, and documented here.
