# Video proxy

`processVideoProxy` streams raw video bytes to clients either from an explicit S3 location (when the request contains a validated object location) or by proxying an origin HTTP/HTTPS URL. It supports single-range requests, forwards important headers, and streams without buffering the whole object in memory.

## Endpoint

- Route: `GET /videos/*` request that is validated and routed to `processVideoProxy`.

## Security

- `processVideoProxy` relies on upstream validation (e.g. `validation.ProcessImageContextFromPath`) to ensure the incoming `params.Url` or `params.CustomObjectKey` is allowed and signed when necessary.
- When `CustomObjectKey` is set, callers must ensure that the key was produced by a server-side signing/validation step; direct client-provided S3 keys are not trusted.

## Key concepts

- Range support: single `bytes=start-end` ranges are supported. Multiple ranges (comma-separated) are rejected.
- S3 explicit location: when `params.CustomObjectKey` is provided and S3 is configured, the handler reads directly from S3 using the MinIO client and may compute ranges after `Stat()` when needed (suffix ranges).
- HTTP proxy: when no explicit S3 location is given the handler performs an HTTP GET to `params.Url`, forwards `Range` if present, and pipes the response.
- Streaming: responses are streamed with `c.SendStream(...)` to minimize memory usage.

## Environment / configuration

- S3 settings (when using explicit location): `S3_ENABLED`, `S3_ENDPOINT`, `S3_BUCKET`, `S3_PREFIX`, etc. The running binary relies on `s3cache` configuration and presence of a MinIO client.
- Request validation and signing: upstream validation must be configured (routes use `validation.ProcessImageContextFromPath`).

## Request details

- Headers accepted: `Range` (single range supported).
- Behavior:
  - If `Range` is present it will be parsed (see `parseRangeHeader`).
  - If `CustomObjectKey` is present, the handler prefers S3 and may use `GetObject` with a range or call `Stat()` to compute suffix ranges.
  - Otherwise, the handler forwards the `Range` header to the origin and relays the response.

## Range handling (summary)

- `parseRangeHeader` supports a single `bytes=start-end` expression and returns `(start, end, hasRange, error)`.
- Suffix ranges like `bytes=-N` are represented as `start = -N, end = -1` and require knowing the total size; the S3 branch uses `obj.Stat()` to compute absolute offsets.
- If the computed range is invalid (start >= size, start > end) the handler returns 416 `Requested Range Not Satisfiable`.

## Headers set or forwarded

- Forwarded from origin or S3 metadata: `Content-Type`, `Content-Length`, `Content-Range`, `Accept-Ranges`.
- Defaults: `Accept-Ranges: bytes` is set when the origin does not provide it.

## Errors and status codes

- 200 OK — full content served.
- 206 Partial Content — range request satisfied.
- 400 / 416 — invalid or unsatisfiable range.
- 500 Internal Server Error — S3 or origin failures (GetObject, Stat, HTTP fetch errors).

## Examples

### HTTP/HTTPS URL (no signature required)

- Request whole object:

```bash
curl -v "http://localhost:3000/videos/<base64-encoded-url>"
```

- Request first 1 KiB:

```bash
curl -v -H "Range: bytes=0-1023" "http://localhost:3000/videos/<base64-encoded-url>"
```

- Request last 512 bytes (suffix range):

```bash
curl -v -H "Range: bytes=-512" "http://localhost:3000/videos/<base64-encoded-url>"
```

### S3 Location (signature required)

When using S3 location, you must provide:
- `loc:{base64-encoded-location}` - S3 object key (from bucket root, no prefix)
- `s:{signature}` - HMAC-SHA256 signature of the location

**Generate signature (example in JavaScript):**

```javascript
const crypto = require('crypto');

const location = 'videos/my-video.mp4';
const key = process.env.APP_HMAC_KEY;

// Create signature: HMAC-SHA256(location, APP_HMAC_KEY)
const signature = crypto
  .createHmac('sha256', key)
  .update(location)
  .digest('hex');

// Base64 URL-safe encode the location
const encodedLocation = Buffer.from(location)
  .toString('base64')
  .replace(/\+/g, '-')
  .replace(/\//g, '_')
  .replace(/=/g, '');

// Build the URL
const url = `http://localhost:3000/videos/loc:${encodedLocation}/s:${signature}`;
console.log(url);
```

**Example S3 location requests:**

- Request whole video from S3:

```bash
# Assuming:
# - location = "videos/my-video.mp4"
# - encodedLocation = "dmlkZW9zL215LXZpZGVvLm1wNA"
# - signature = "abc123def456..."

curl -v "http://localhost:3000/videos/loc:dmlkZW9zL215LXZpZGVvLm1wNA/s:abc123def456..."
```

- Request first 1 KiB from S3:

```bash
curl -v -H "Range: bytes=0-1023" "http://localhost:3000/videos/loc:dmlkZW9zL215LXZpZGVvLm1wNA/s:abc123def456..."
```
