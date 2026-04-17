# Overview

This project adds a PATCH endpoint to an existing todo management system that allows clients to mark a specific todo item as completed. The endpoint provides a focused, partial-update operation distinct from full-resource replacement, enabling lightweight state transitions on individual todo items.

The target users are API consumers (frontend applications, integrations, and end users via clients) who need a simple, reliable way to flip a todo's status to "completed" without resending the entire resource. The approach emphasizes correctness, idempotency, and clear error handling so that completion actions are predictable and safe to retry.

# Capabilities

## Endpoint Behavior
- Expose a PATCH endpoint at `/todos/{id}` (or equivalent resource path) that accepts a request to mark a todo as completed.
- Accept a request body indicating the completion state change (e.g., `{ "completed": true }`).
- Update only the completion status field; leave all other fields of the todo unchanged.
- Return the updated todo resource in the response body upon success.
- Respond with HTTP 200 on a successful update.
- Operation must be idempotent: marking an already-completed todo as completed returns success without error.
- Record a `completedAt` timestamp when the todo transitions to completed.
- Allow toggling back to not-completed if `completed: false` is supplied, clearing the `completedAt` timestamp.

## Validation
- Validate that the `id` path parameter refers to an existing todo; return HTTP 404 if not found.
- Validate the request body is well-formed JSON; return HTTP 400 for malformed input.
- Validate that the `completed` field is a boolean; reject other types with HTTP 400 and a descriptive error message.
- Reject requests containing fields other than `completed` with HTTP 400 (or ignore them based on documented policy).
- Return HTTP 415 if the request content type is not JSON.

## Authentication & Authorization
- Require the request to be authenticated; return HTTP 401 for unauthenticated requests.
- Ensure only users authorized to modify the targeted todo can complete it; return HTTP 403 otherwise.

## Error Handling
- Return structured error responses including an error code, human-readable message, and the offending field where applicable.
- Return HTTP 500 with a generic error message for unexpected server errors, without leaking internal details.
- Log all failed update attempts with sufficient context for debugging.

## Data Integrity
- Persist the completion state change durably before responding with success.
- Ensure concurrent PATCH requests on the same todo result in a consistent final state.
- Do not modify the todo's creation timestamp, owner, or identifier as part of this operation.

## Observability
- Emit a log entry for each successful completion state change, including todo id and actor.
- Expose metrics for endpoint request count, latency, and error rate.

## Performance
- Respond to valid requests within 300ms at the 95th percentile under expected load.
- Support concurrent updates to different todos without contention.

## Documentation
- Publish API documentation describing the endpoint, request schema, response schema, and all possible status codes.
- Provide example request and response payloads for both completion and un-completion scenarios.
