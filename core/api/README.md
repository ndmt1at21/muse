# Core OpenAPI

[`openapi.swagger.json`](openapi.swagger.json) is the OpenAPI 2.0 (Swagger) spec
for Core's REST surface — every `game.v1` RPC exposed under `/api/v1` by the
grpc-gateway. It is **generated**, not hand-written.

## Regenerate

```bash
make generate   # buf generate — emits Go + this spec
```

Driven by [`buf.gen.yaml`](../../buf.gen.yaml) (the `protoc-gen-openapiv2`
plugin) from [`proto/game/v1/engine.proto`](../../proto/game/v1/engine.proto),
then post-processed by [`envelope.jq`](envelope.jq) (see below). Do not edit the
JSON by hand — change the proto / jq and run `make generate`.

## Reading the spec

- **Envelope** — every response is the uniform envelope
  `{ code, message, trace_id, data }`. `protoc-gen-openapiv2` only sees the raw
  proto messages (the gateway wraps them at runtime via a response rewriter), so
  [`envelope.jq`](envelope.jq) rewrites each operation after generation: success
  responses become the envelope with `data` = the proto response; errors become
  `apiErrorEnvelope` (`data.error = { status, reason, domain, … }`).
- **Enums** — closed-set fields serialize as the proto value **name**
  (`CAMPAIGN_STATUS_ACTIVE`), as the spec's `enum` lists show.
- **Field names** — `snake_case` (`json_names_for_fields=false`), matching the
  gateway's `UseProtoNames`.
- **Timestamps** — every time field (`created_at`, `updated_at`, `start_date`,
  `expires_at`, …) is an `int64` **unix timestamp in seconds**; `0` means unset.
  Per proto3 JSON, Core's direct REST encodes int64 as a quoted string
  (`"created_at": "1717200000"`); the reference BFFs re-emit it as a JSON number.
- **Scope & auth** — Core is auth-agnostic; `scope.tenant_id` / `scope.merchant_id`
  are ordinary request fields, not security schemes.
