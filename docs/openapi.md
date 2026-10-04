# OpenAPI contract

## What is `openapi.yaml`?

[OpenAPI](https://spec.openapis.org/oas/v3.1.0) is a standard, machine-readable
description of an HTTP API. YAML is the text format used to write this project's
contract. It tells developers and tools which URLs/methods exist, what parameters
they accept, and what response bodies, status codes, and headers to expect.

Ticketopia's source contract is [internal/api/openapi.yaml](../internal/api/openapi.yaml).
It describes the implemented discovery API, its own contract-download route, and
the `/healthz` and `/readyz` probes. It does not describe HTML pages or implement
planned account/community APIs. See [discovery](discovery.md#json-api) for usage
and [deployment](deployment.md#health-and-shutdown) for probe semantics.

For example, the event-search contract documents `limit` as an integer from 1 to
100, the paginated `items`/`next_cursor` response, freshness metadata, and structured
400/503 errors. Tools can read those rules without understanding the Go source.

## File structure and versions

- `openapi: 3.1.0` identifies the **OpenAPI specification format**, not the Go or
  Node version.
- `info` describes the API's title, description, license, and contract version
  (currently `1.1.0`). That version is distinct from the `/api/v1` URL namespace.
- `servers` identifies the API base URL; `/` makes it relative to the serving host.
- `paths` lists routes, methods, parameters, response codes, and media types.
- `components` holds reusable schemas and error responses. `$ref` links reuse
  those definitions rather than repeating them at each endpoint.
- `security: []` describes the current public API without client authentication.
  The server-side `TICKETMASTER_KEY` is an upstream credential, not a client token
  to put in this document.

## How Ticketopia serves it

`internal/api/api.go` embeds the YAML using `//go:embed openapi.yaml` and serves
those bytes at **GET `/api/v1/openapi.yaml`** as `application/yaml`. No Ticketmaster
or cache request is needed to download it. The route accepts no query parameters.
From the host machine, with the server running:

```sh
curl http://localhost:8080/api/v1/openapi.yaml
```

The embedded document ships in the Go binary and container; it is not a separate
runtime configuration file. Rebuild and restart the application after editing it.

The contract is **maintained by hand**, alongside Echo handlers and Go models.
It does not automatically register routes, validate requests, generate Go code,
or guarantee that responses match its schemas. Runtime behavior is implemented
in Go and verified with tests. Ticketopia does not currently serve a Swagger UI
or generate an API client from this document.

## Viewing, validating, and updating

OpenAPI-compatible tools such as Swagger UI, Redoc, and Postman can import the
file to display API documentation or help construct requests. Client generators
can also consume it; generated clients are not currently part of this repository.

Using the project's **Node.js 24** toolchain, validate syntax, references, and API
documentation rules with:

```sh
npx --yes @redocly/cli lint internal/api/openapi.yaml
```

The default recommended rules currently emit two `operation-4xx-response`
warnings for `/healthz` and `/readyz`. The GET probes have no request-validation
errors to document; the contract is valid despite those warnings. Do not invent
unsupported responses just to satisfy a generic lint rule.

When changing an HTTP API:

1. Update the Go route/handler, validation, and models as needed.
2. Update the matching `paths` and `components` definitions, including error cases
   and headers. Document implemented behavior, not future plans.
3. Add or update behavior tests; linting alone cannot detect implementation drift.
4. Run the linter, `go test -race ./...`, and `go vet ./...` with Go 1.27+.
5. Rebuild using `npm run build` or `docker build -t ticketopia:local .` and restart
   the server so clients receive the updated embedded contract.
