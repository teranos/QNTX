# Review: Error Types Over gRPC Boundary

## Current State

The `errors` package defines Go-only sentinel error types:
- `ErrNotFound`, `ErrInvalidRequest`, `ErrUnauthorized`, `ErrForbidden`
- `ErrServiceUnavailable`, `ErrTimeout`, `ErrConflict`, `ErrMethodNotAllowed`

These sentinels are checked with `errors.Is()` and carry semantic meaning within a single Go process.

Plugins communicate via gRPC. Errors cross the boundary as standard gRPC status codes (`codes.NotFound`, `codes.InvalidArgument`, etc.) with string messages. There is **no proto-level mapping** between QNTX error sentinels and gRPC status codes.

## Gap

When a plugin returns `status.Error(codes.NotFound, "some message")`, the server receives a gRPC error. There is no automatic mapping to `errors.ErrNotFound` — the server would need to manually inspect `status.Code(err)` and wrap with the appropriate sentinel.

This means `errors.IsNotFoundError(err)` won't work on errors received from plugins unless someone explicitly maps them.

## Adjustment

Define a bidirectional mapping between QNTX error sentinels and gRPC status codes:

| QNTX Sentinel | gRPC Code |
|---|---|
| `ErrNotFound` | `codes.NotFound` |
| `ErrInvalidRequest` | `codes.InvalidArgument` |
| `ErrUnauthorized` | `codes.Unauthenticated` |
| `ErrForbidden` | `codes.PermissionDenied` |
| `ErrMethodNotAllowed` | `codes.Unimplemented` |
| `ErrServiceUnavailable` | `codes.Unavailable` |
| `ErrTimeout` | `codes.DeadlineExceeded` |
| `ErrConflict` | `codes.AlreadyExists` |

Options:
1. **Interceptor approach**: gRPC server/client interceptors that automatically convert between QNTX sentinels and gRPC status codes at the boundary.
2. **Proto enum approach**: Define a `QNTXErrorCode` enum in proto, carried as error metadata. Richer than gRPC codes but requires proto changes.

Interceptor approach is lower friction and aligns with standard gRPC patterns.
