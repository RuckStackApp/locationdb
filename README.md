# locationdb
## locationdb

Go module:

```text
github.com/ruckstackapp/locationdb
```

This repository will host the durable database layer built on top of
`locationindex`.

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
