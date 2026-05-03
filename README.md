# locationdb
## locationdb

Go module:

```text
github.com/ruckstackapp/locationdb
```

This repository will host the durable database layer built on top of
`locationindex`.

Per-store records are persisted in a compact binary format and are treated as
the service source of truth. Location indexes are durable acceleration
structures that can be reopened or rebuilt from stored records.

The service also includes an embedded admin UI. The Svelte source lives in
`ui/`, TypeScript API bindings live in `ts/`, and the Go binary serves the
built frontend from embedded assets.

Initial scope:

- multiple independently configured stores
- JSON query request model
- future query-language entry point

Example JSON query:

```json
{
  "near": {
    "lat": 43.65,
    "lon": -79.38,
    "radius": 2000
  },
  "labels": ["restaurant"],
  "valid_at": "2026-05-03T12:00:00Z",
  "limit": 50
}
```

Future query-language shape:

```text
NEAR(43.65, -79.38, 2000)
AND label IN ("restaurant")
AND valid_at = "2026-05-03T12:00:00Z"
```

Records can be inserted with either a precomputed location code or raw
coordinates. When `lat` and `lon` are provided, the service will encode them
into a location code using either the provided record precision or the store's
configured spatial precision.
