// Module gamekit is the pure, transport-free game engine SDK (Mode A).
// It depends only on the Go standard library — no DB, Redis, gRPC, or HTTP.
// Consumers implement the ports (or import github.com/muse/adapters) and bring
// their own API. This module is independently versioned (semver).
module github.com/muse/gamekit

go 1.25
