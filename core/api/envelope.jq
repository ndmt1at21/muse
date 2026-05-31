# Post-processes the protoc-gen-openapiv2 output so the documented responses match
# what the gateway actually returns: the uniform envelope { code, message, trace_id, data }.
# protoc-gen-openapiv2 only sees the raw proto response messages (the gateway wraps them
# at runtime via a ForwardResponseRewriter), so we wrap them here. Run after `buf generate`.
#
#   jq -f core/api/envelope.jq core/api/openapi.swagger.json > tmp && mv tmp ...
#
# Success "200": data = the original response schema. Error "default": the error envelope.

# Envelope field schemas shared by success + error.
def envProps(dataSchema):
  {
    "code":     { "type": "integer", "format": "int32" },
    "message":  { "type": "string" },
    "trace_id": { "type": "string" },
    "data":     dataSchema
  };

# Add the error-envelope definitions (mirrors pkg/envelope).
.definitions += {
  "apiFieldViolation": {
    "type": "object",
    "properties": {
      "field":       { "type": "string" },
      "description": { "type": "string" }
    }
  },
  "apiErrorInfo": {
    "type": "object",
    "description": "data.error payload (mirrors google.rpc.ErrorInfo).",
    "properties": {
      "status":           { "type": "string", "description": "canonical gRPC status name" },
      "reason":           { "type": "string", "description": "stable machine reason (see docs/ERRORS.md)" },
      "domain":           { "type": "string" },
      "metadata":         { "type": "object", "additionalProperties": { "type": "string" } },
      "field_violations": { "type": "array", "items": { "$ref": "#/definitions/apiFieldViolation" } }
    }
  },
  "apiErrorData": {
    "type": "object",
    "properties": { "error": { "$ref": "#/definitions/apiErrorInfo" } }
  },
  "apiErrorEnvelope": {
    "type": "object",
    "description": "Uniform error envelope.",
    "properties": (envProps({ "$ref": "#/definitions/apiErrorData" }))
  }
}
# Wrap each operation's responses in the envelope.
| .paths |= map_values(
    map_values(
      if type == "object" and has("responses") then
        ( if .responses["200"] and .responses["200"].schema
          then .responses["200"].schema |=
            { "type": "object", "properties": envProps(.) }
          else . end )
        | ( if .responses["default"]
            then .responses["default"].schema = { "$ref": "#/definitions/apiErrorEnvelope" }
            else . end )
      else . end
    )
  )
