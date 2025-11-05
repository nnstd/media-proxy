# Multi-Part Video Upload

This document describes the multi-part video upload API supported by the media-proxy service.
It enables uploading large video files in parts (chunks), tracking progress in Redis, and storing parts in S3.

The server provides three endpoints:
- Initialize a multi-part upload: POST /videos/multiparts
- Upload a single part: POST /videos/multiparts/:uploadId/parts/:partIndex
- Check upload status: GET /videos/multiparts/:uploadId

Security: 
- The initialization endpoint requires `token` query parameter matching `APP_TOKEN`.
- Part upload requires `uploadToken` (a unique token generated per upload session, returned by init).
- Status check requires `token` query parameter matching `APP_TOKEN`.
- Uploading must be enabled via `APP_UPLOADING_ENABLED=true` and S3 must be configured.
- Redis is required for upload tracking when using multi-part uploads.

## Key concepts

- uploadId: A server-generated identifier for the multi-part upload session. Returned by the initialization endpoint.
- uploadToken: A secure random token generated per upload session. Use this to authenticate part uploads (not APP_TOKEN).
- parts: The file is split into N parts according to the configured chunk size. Each part has index, offset and size.
- partIndex: Zero-based index of a part in the upload session.
- chunkSize: The per-part size in bytes. Default is 80 MB (80 * 1024 * 1024). Can be overridden during init.
- Redis: The upload session metadata is stored in Redis (key prefix `upload:`). The service uses this to track which parts are uploaded.
- S3: Each uploaded part is stored in S3 at `{location}.part{index}` (where `location` is the requested object key).

## Environment / configuration

- APP_UPLOADING_ENABLED=true (enable uploading)
- APP_TOKEN (authentication token used by clients)
- APP_HMAC_KEY (not required for multipart init; used for URL signatures elsewhere)
- S3_ENABLED, S3_ENDPOINT, S3_ACCESS_KEY_ID, S3_SECRET_ACCESS_KEY, S3_BUCKET, S3_SSL, S3_PREFIX
- REDIS_ENABLED, REDIS_ADDR, REDIS_PASSWORD, REDIS_DB
- APP_CHUNK_SIZE (optional default chunk size in bytes)

## 1) Initialize multi-part upload

Endpoint
```
POST /videos/multiparts?token={token}&deadline={deadline}&location={location}&size={bytes}&contentType={mime}&chunkSize={bytes}
```

Query parameters
- token (required) — must match `APP_TOKEN`.
- deadline (required) — upload expiration; server accepts RFC3339 or a unix timestamp. Upload parts after this time will be rejected.
- location (required) — desired S3 object key (string). The server will sanitize and validate it (no "..", limited charset).
- size (required) — total file size in bytes.
- contentType (required) — MIME type of the video (must be a recognized video MIME type).
- chunkSize (optional) — override per-part size in bytes (defaults to server default; typically 80MB).

Response (200 OK)
```json
{
  "uploadId": "<upload-id>",
  "uploadToken": "<secure-random-token>",
  "location": "videos/user123/my-video.mp4",
  "totalSize": 157286400,
  "chunkSize": 83886080,
  "partsCount": 2,
  "parts": [
    { "index": 0, "offset": 0, "size": 83886080 },
    { "index": 1, "offset": 83886080, "size": 73400320 }
  ],
  "expiresAt": "2025-11-03T12:00:00Z"
}
```

Errors
- 400 Bad Request: missing/invalid params (size, deadline, contentType, location)
- 403 Forbidden: invalid token or deadline expired
- 413 Request Entity Too Large: size exceeds configured `APP_MAX_VIDEO_SIZE_MB`
- 503 Service Unavailable: Redis or S3 not configured

Notes
- The server computes partsCount = ceil(size / chunkSize) and returns an array of part metadata with offsets and sizes.
- The server generates a unique `uploadToken` for this session. Save this token - you'll need it to upload parts.
- Upload tracking is stored in Redis under the key `upload:{uploadId}` and kept until the upload `expiresAt` (or a configured TTL).

## 2) Upload a part

Endpoint
```
POST /videos/multiparts/:uploadId/parts/:partIndex?uploadToken={uploadToken}
```

Path parameters
- uploadId (required) — upload session id returned by init
- partIndex (required) — zero-based part index to upload

Query parameters
- uploadToken (required) — the unique token returned by the init endpoint (NOT APP_TOKEN)

Form data
- video (required) — the multipart `video` field containing raw bytes for this part. The server expects the part size to exactly match the declared size for this part.

Behavior
- The server validates the `uploadToken` against the stored upload session.
- The server reads the `uploadId` info from Redis, validates the `partIndex` and the part size.
- The part is stored in S3 under `{location}.part{partIndex}` using the configured S3 client.
- The server marks the part as uploaded in Redis.
- If marking results in all parts being present, the server returns `complete: true` (server may merge parts asynchronously or via a separate step).

Response (200 OK)
```json
{
  "uploadId": "1234567890-videos/user123/video.mp4",
  "partIndex": 0,
  "size": 83886080,
  "complete": false
}
```

When the last part is uploaded:
```json
{
  "uploadId": "1234567890-videos/user123/video.mp4",
  "partIndex": 1,
  "size": 73400320,
  "complete": true
}
```

Errors
- 400 Bad Request — missing/invalid path params or multipart field
- 401/403 Forbidden — missing/invalid token
- 404 Not Found — upload not found or expired
- 409 Conflict — part already uploaded (current implementation simply ignores duplicate marks)
- 413 Request Entity Too Large — part size mismatch
- 500 Internal Server Error — S3/Redis errors

Important
- The client must send exactly the bytes for the intended part (size must match the `parts` array returned by init).
- If an upload is interrupted, re-uploading the same part is allowed (the server will detect and not duplicate entries in the uploaded list).

## 3) Check upload status

Endpoint
```
GET /videos/multiparts/:uploadId?token={token}
```

Path parameters
- uploadId (required) — upload session id

Query parameters
- token (required) — must match `APP_TOKEN`

Response (200 OK)
```json
{
  "uploadId": "1234567890-videos/user123/video.mp4",
  "location": "videos/user123/video.mp4",
  "totalSize": 157286400,
  "partsCount": 2,
  "uploadedParts": [0, 1],
  "uploadedCount": 2,
  "complete": true,
  "contentType": "video/mp4",
  "createdAt": "2025-11-03T11:00:00Z",
  "expiresAt": "2025-11-03T12:00:00Z"
}
```

Errors
- 400 Bad Request — missing uploadId
- 403 Forbidden — invalid token
- 404 Not Found — upload not found or expired

## Merging parts (server-side)

Current behavior stores parts in S3 with `.partN` suffix. There is a placeholder where parts can be merged into the final object (e.g. server-side concatenation or S3 multipart compose operation). The server logs when the upload completes and returns `complete: true` to the client. Implementing the merge can be done in a background worker and is intentionally decoupled from the part upload endpoint.

Suggested approaches for merging parts:
- Use S3 multipart upload APIs (preferred): create a multipart upload on S3 and upload parts directly to it (or copy the .partN objects as parts), then CompleteMultipartUpload.
- If using MinIO/S3 that supports server-side Compose/Concatenate, use that to assemble the final object.
- Simple fallback: download parts and upload concatenated result (inefficient for large files).

## Examples

### cURL: initialize
```bash
curl -X POST \
  "http://localhost:3000/videos/multiparts?token=${APP_TOKEN}&deadline=$(date -u -d "+5 hours" +%s)&location=videos/user123/my-video.mp4&size=157286400&contentType=video/mp4" \
  -i
```

### cURL: upload part 0
```bash
curl -X POST \
  "http://localhost:3000/videos/multiparts/<UPLOAD_ID>/parts/0?uploadToken=${UPLOAD_TOKEN}" \
  -F "video=@part0.bin" \
  -i
```

### Node.js: initialize + upload (high-level)
See `example/multipart-upload.js` in the repository for a complete example showing how to calculate parts, initialize an upload, upload parts and check status.

## Client-side behavior recommendations
- Use the `parts` array returned by the init endpoint to read exact offsets and sizes from the file.
- Upload parts in parallel if desired, but do not exceed available memory.
- Retry individual parts on transient errors; the server is idempotent for duplicate part marks.
- Poll the status endpoint or rely on server-sent notifications to detect completion if you need post-processing to start.

## Notes and limitations
- The current implementation stores parts as separate objects in S3 with `.partN` suffix. The merge step is not implemented in the upload endpoint and must be added separately.
- Redis must be available and reachable; if Redis configuration is missing, multi-part endpoints will return 503.
- The server validates `contentType` and file sizes.

---

If you want, I can also:
- Add a README link to this file (quick change)
- Implement server-side merging using S3 multipart compose
- Add unit tests for Redis upload tracking
