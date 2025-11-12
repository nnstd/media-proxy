# Image upload

`processImageUpload` accepts multipart/form-data image uploads or proxies an upload operation to store processed results in S3 when callers provide a validated object location. It supports two validation modes (token and signature), forwards important headers, streams without buffering the whole file in memory, and can upload the processed result to an explicit S3 location when requested.

## Endpoint

- Route: `POST /images` request that is validated and routed to `processImageUpload` (or a multipart route that calls it).

## Security and validation methods

Two validation modes are supported for requests that reference an S3 location (so-called "explicit location"):

- Token validation
  - The client includes a short-lived token (for example as a query parameter `t:<token>` or in an `Authorization: Bearer <token>` header) that the server verifies against an authentication/authorization service or local HMAC check.
  - Tokens typically encode claims about the uploader (user id, allowed operations), expiration, and the target object path. The server must validate expiration and permitted operations before accepting or writing to the S3 location.

- Signature validation
  - The client includes an HMAC-SHA256 signature of the S3 object key (or a location payload) as a query parameter `s:<signature>` alongside a base64-url-encoded location `loc:<encoded-location>`.
  - The server recomputes HMAC-SHA256(location, APP_HMAC_KEY) and compares it to the provided signature. If they match and other checks (expiration / allowed prefix) pass, the location is trusted.
  - Signature-based validation is stateless and simple to implement when the client can obtain a signed location from a trusted backend.

Notes on choosing a method
- Use token validation when you need per-user authorization, scoped permissions, or revocation. Use signatures when you want a lightweight, stateless way to allow uploads to a specific object key that was generated server-side.

## S3 result upload (explicit location)

- When the request provides a validated explicit S3 location (via `loc:` + `s:` or a validated token containing a location), `processImageUpload` will upload the final processed image to the configured S3 backend instead of returning the image bytes directly.
- The upload writes to the object key relative to the configured `S3_PREFIX` and `S3_BUCKET` and may set common metadata such as `Content-Type` and `Content-Length`.
- If the request contains `Range` headers, they are ignored for uploads; ranges are relevant for proxy/download endpoints but not for multipart upload.
- Errors from S3 (PutObject failures, permissions, network errors) translate to 5xx responses. The handler attempts to surface useful error messages but does not leak secrets.

## Key concepts

- Multipart streaming: incoming multipart/form-data parts are streamed and processed without loading the full file into memory.
- Validation before write: the server always validates the provided token or signature and any other request-level guards before performing the S3 upload.
- Atomic writes: when possible the handler performs an upload that is either completed or fails; incomplete multipart state is not left behind.

## Environment / configuration

- S3 settings (required for explicit location uploads and result storage): `S3_ENABLED`, `S3_ENDPOINT`, `S3_BUCKET`, `S3_PREFIX`, `S3_ACCESS_KEY`, `S3_SECRET_KEY`, and possibly `S3_REGION`.
- HMAC key for signature validation: `APP_HMAC_KEY` (used to sign/verify explicit locations).
- Token verification settings: whatever upstream token/identity service or local secret the server uses.

## Request details

- Accepted content-type for upload: `multipart/form-data` with a file part (commonly named `file`).
- Optional parameters (examples of how they may be passed in path or query):
  - `loc:<base64-url-encoded-location>` — explicit S3 object key (relative to bucket root)
  - `s:<signature>` — HMAC-SHA256 signature of the location
  - `t:<token>` — short-lived token for token-based validation
- Behavior:
  - If an explicit `loc` (and signature or valid token) is present and verified, the server uploads the processed image to S3 at the requested key and returns a 200 with a JSON body containing the object key / URL (or 201 Created depending on your API choice).
  - If no explicit location is provided, the handler returns the processed image bytes in the HTTP response body (streamed) with an appropriate `Content-Type`.

## Headers set or forwarded

- On returned image responses: `Content-Type` (set by processing code), `Content-Length` when known.
- On successful explicit-location uploads: a JSON response with fields such as `bucket`, `key`, `url` (pre-signed URL or path), and `content_type` is recommended.

## Errors and status codes

- 200 OK — upload succeeded and server returns JSON with location info (or returns processed bytes if no explicit location was requested).
- 201 Created — optional for explicit-location writes.
- 400 Bad Request — malformed multipart, missing file part, or invalid parameters.
- 401 / 403 — token or signature validation failure, or unauthorized to write to the requested location.
- 500 Internal Server Error — S3 upload failure, processing errors.

## Examples

### Direct multipart upload (no explicit location)

Upload and get processed image bytes back in the response.

curl -v -F "file=@./my-photo.jpg" "http://localhost:3000/images"

### Upload with explicit S3 location using signature

When using S3 location, you must provide:
- `loc:{base64-encoded-location}` - S3 object key (from bucket root, no prefix)
- `s:{signature}` - HMAC-SHA256 signature of the location

Generate signature (example in JavaScript):

```javascript
const crypto = require('crypto');

const location = 'uploads/user-123/photo.jpg';
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

const url = `http://localhost:3000/images/loc:${encodedLocation}/s:${signature}`;
console.log(url);
```

Then upload:

curl -v -F "file=@./my-photo.jpg" "http://localhost:3000/images/loc:dXBsb2Fkcy91c2VyLTEyMy9waG90by5qcGc/s:abcdef012345..."

### Upload with token validation

If your system issues short-lived tokens for uploads, include the token as a query param or Authorization header. Example (query param):

curl -v -F "file=@./my-photo.jpg" "http://localhost:3000/images?loc:dXBsb2Fkcy91c2VyLTEyMy9waG90by5qcGc&t:eyJhbGciOi..."

Or with Authorization header:

curl -v -H "Authorization: Bearer eyJhbGciOi..." -F "file=@./my-photo.jpg" "http://localhost:3000/images?loc:dXBsb2Fkcy91c2VyLTEyMy9waG90by5qcGc"

## Implementation notes and edge cases

- Validate location prefix: always check that the provided location resides under allowed prefixes to avoid overwriting unrelated objects.
- Token expiry and revocation: tokens should be short-lived and checked against a revocation list when applicable.
- Large files: stream multipart parts and use buffered uploads or multipart S3 uploads if the runtime memory is limited.
- Partial or failed uploads: ensure cleanup or atomic writes to avoid leaving half-written objects.

## Try it

Start the server (example, depends on your build):

```bash
# build and run the Go binary in this project
go run main.go
```

Then try the curl examples above.

## Summary

`processImageUpload` supports both returning processed image bytes directly and writing results to S3 when the client provides a validated explicit location. Use token validation for fine-grained authorization and signatures for a simple stateless allow-list. Ensure S3 configuration and `APP_HMAC_KEY` are set when using explicit locations.
