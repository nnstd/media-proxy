````markdown
# Image proxy

`processImageProxy` streams raw image bytes to clients either from an explicit S3 location (when the request contains a validated object location) or by proxying an origin HTTP/HTTPS URL. It supports forwarding important headers and streams without buffering the whole object in memory.

## Endpoint

- Route: `GET /images/*` request that is validated and routed to `processImageProxy`.

## Security

- `processImageProxy` relies on upstream validation (e.g. `validation.ProcessImageContextFromPath`) to ensure the incoming `params.Url` or `params.CustomObjectKey` is allowed and signed when necessary.
- When `CustomObjectKey` (a validated S3 object key) is set, callers must ensure that the key was produced by a server-side signing/validation step; direct client-provided S3 keys are not trusted.

## Key concepts

- S3 explicit location: when `params.CustomObjectKey` is provided and S3 is configured, the handler reads directly from S3 using the MinIO client (or configured S3 client) and may perform ranged GETs as requested.
- HTTP proxy: when no explicit S3 location is given the handler performs an HTTP GET to `params.Url` and pipes the response back to the client.
- Streaming: responses are streamed (e.g. via `c.SendStream(...)`) to minimize memory usage and avoid loading the entire image into RAM.

## Environment / configuration

- S3 settings (when using explicit location): `S3_ENABLED`, `S3_ENDPOINT`, `S3_BUCKET`, `S3_PREFIX`, etc. The running binary relies on S3 client configuration.
- Request validation and signing: upstream validation must be configured (routes commonly use `validation.ProcessImageContextFromPath` or equivalent middleware to enforce signatures and allowed origins).

## Request details

- Headers accepted: `Range` (single range supported by the proxy if implemented), `If-Modified-Since`, etc. Note: many image use-cases request the whole object; range support is optional but useful for large assets.
- Behavior:
  - If `CustomObjectKey` is present, the handler prefers S3 and will `GetObject` (optionally with a range) or use `Stat()` to compute suffix ranges when needed.
  - Otherwise, the handler forwards the request to the origin `params.Url` and relays the response.

## Validation with signature (S3 explicit location)

When you want the proxy to read an image directly from S3, don't allow clients to provide raw S3 object keys. Instead:

- The server-side flow:
  1. Server generates a canonical object key (for example `images/<user-id>/<image-name>.jpg`).
  2. Server signs that key using an HMAC-SHA256 keyed by `APP_HMAC_KEY` (or similar secret).
  3. The signed payload is sent to the client (encoded) or embedded in URLs so the proxy can verify it.

- The proxy expects two parameters embedded in the path (examples use URL path segments):
  - `loc:{base64-encoded-location}` — base64 URL-safe encoded S3 object key (relative to bucket root, without any server `S3_PREFIX`).
  - `s:{signature}` — hex-encoded HMAC-SHA256 signature of the raw location string.

- On request the proxy path validation middleware must:
  - Decode the `loc` value to get the original object key.
  - Compute HMAC-SHA256(location, APP_HMAC_KEY) and compare it to `s` in constant time.
  - Reject requests where the signature does not match (HTTP 400/401) or where the decoded location contains disallowed segments (e.g. `..`).

**Signature generation example (JavaScript):**

```javascript
const crypto = require('crypto');

const location = 'images/12345/photo.jpg';
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
  .replace(/=+$/, '');

// Build the URL
const url = `http://localhost:3000/images/loc:${encodedLocation}/s:${signature}`;
console.log(url);
```

## Reading from S3 (explicit location)

- When the proxy receives a validated `CustomObjectKey` (decoded from `loc:` and verified via `s:`), it will read directly from S3 rather than fetching the origin URL.
- Benefits: lower latency, reduced egress from origin servers, and the ability to leverage S3 range reads for partial content.
- Implementation notes:
  - Use the configured S3 client to `GetObject` using the validated key. If a `Range` header is present, pass a corresponding range to S3.
  - For suffix ranges (e.g. `bytes=-N`) S3 `Stat()` (or `HeadObject`) can be used to determine the object size and compute an absolute range.
  - Forward `Content-Type`, `Content-Length`, `Content-Range` and `Accept-Ranges` from the S3 response or set sensible defaults.

## Headers set or forwarded

- Forwarded from origin or S3 metadata: `Content-Type`, `Content-Length`, `Content-Range`, `Accept-Ranges`.
- Defaults: `Accept-Ranges: bytes` may be set when the origin does not provide it.

## Errors and status codes

- 200 OK — full content served.
- 206 Partial Content — range request satisfied.
- 400 / 401 — invalid signature or malformed `loc`.
- 416 — invalid or unsatisfiable range (when ranges are supported and invalid).
- 500 Internal Server Error — S3 or origin failures (GetObject, Stat, HTTP fetch errors).

## Examples

### HTTP/HTTPS URL (no signature required)

- Request whole image:

```bash
curl -v "http://localhost:3000/images/<base64-encoded-url>"
```

### S3 Location (signature required)

When using S3 location, you must provide:
- `loc:{base64-encoded-location}` - S3 object key (from bucket root, no prefix)
- `s:{signature}` - HMAC-SHA256 signature of the location

**Generate signature (example in JavaScript):**

```javascript
// See signature example above — identical for images
```

**Example S3 location requests:**

- Request whole image from S3:

```bash
# Assuming:
# - location = "images/12345/photo.jpg"
# - encodedLocation = "aW1hZ2VzLzEyMzQ1L3Bob3RvLmpwZw"
# - signature = "abc123def456..."

curl -v "http://localhost:3000/images/loc:aW1hZ2VzLzEyMzQ1L3Bob3RvLmpwZw/s:abc123def456..."
```

- Request a byte range from S3 (if supported):

```bash
curl -v -H "Range: bytes=0-1023" \
  "http://localhost:3000/images/loc:aW1hZ2VzLzEyMzQ1L3Bob3RvLmpwZw/s:abc123def456..."
```

````
