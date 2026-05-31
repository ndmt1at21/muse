// Package enumx converts between the proto enums in game.v1 and the lowercase
// string tokens used elsewhere.
//
// Two boundaries need different string forms:
//
//   - The persistence/domain layer (gamekit/types, sqlstore) stores the
//     historical lowercase token, e.g. "active", "magic_link". Use Token /
//     FromToken with the enum's value-name prefix (e.g. "GAME_STATUS_") to map
//     proto enum <-> domain token. The zero (UNSPECIFIED) value maps to "".
//
//   - The wire/JSON contract emits the proto value name verbatim, e.g.
//     "GAME_STATUS_ACTIVE" (protojson default). BFFs that speak that contract
//     use Name / Parse, which need no prefix.
package enumx

import "strings"

// Enum is any proto3-generated enum: an int32 with a String() method.
type Enum interface {
	~int32
	String() string
}

// Name returns the proto value name ("" for the zero/UNSPECIFIED value).
func Name[E Enum](e E) string {
	if e == 0 {
		return ""
	}
	return e.String()
}

// Parse resolves a proto value name (e.g. "GAME_STATUS_ACTIVE") to its enum,
// returning the zero value for "" or an unknown name.
func Parse[E Enum](name string, valueByName map[string]int32) E {
	return E(valueByName[name])
}

// Token returns the lowercase domain token for an enum: the value name with its
// prefix stripped and lowercased (e.g. GAME_STATUS_ACTIVE + "GAME_STATUS_" ->
// "active"). The zero/UNSPECIFIED value maps to "".
func Token[E Enum](e E, prefix string) string {
	if e == 0 {
		return ""
	}
	return strings.ToLower(strings.TrimPrefix(e.String(), prefix))
}

// FromToken resolves a lowercase domain token back to its enum, returning the
// zero value for "" or an unknown token.
func FromToken[E Enum](token, prefix string, valueByName map[string]int32) E {
	if token == "" {
		return 0
	}
	return E(valueByName[prefix+strings.ToUpper(token)])
}
