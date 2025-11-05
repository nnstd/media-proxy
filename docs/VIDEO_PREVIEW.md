# Video Preview

`processVideoPreview` extracts a frame from a video (from either an explicit S3 location or an HTTP/HTTPS URL) and returns it as an image. The image can be resized, rescaled, and converted to WebP or JPEG format with configurable quality. Both memory cache and S3 cache are supported for the generated preview images.

## Endpoint

- Route: `GET /videos/preview/*` - requests are validated and routed to `processVideoPreview`.

## Security

- `processVideoPreview` relies on upstream validation (e.g. `validation.ProcessImageContextFromPath`) to ensure the incoming `params.Url` or `params.CustomObjectKey` is allowed and properly signed when necessary.
- When `CustomObjectKey` is set, callers must ensure that the key was produced by a server-side signing/validation step; direct client-provided S3 keys are not trusted.
- S3 presigned URLs are generated with 1-hour expiration for ffmpeg to access the video.

## Key concepts

- Frame extraction: uses ffmpeg to extract a single frame at a specified position (first frame, middle frame, or last frame).
- Image processing: extracted frames can be resized, rescaled, and encoded to WebP or JPEG.
- S3 explicit location: when `params.CustomObjectKey` is provided and S3 is configured, the handler reads the video directly from S3 using a presigned URL.
- HTTP proxy: when no explicit S3 location is given, the handler validates and extracts frames from `params.Url`.
- Caching: preview images are cached in both memory (Ristretto) and S3 (if enabled) to optimize performance.

## Environment / configuration

- S3 settings (when using explicit location): `S3_ENABLED`, `S3_ENDPOINT`, `S3_BUCKET`, `S3_PREFIX`, etc. The running binary relies on `s3cache` configuration and presence of a MinIO client.
- Cache settings: `APP_CACHE_TTL` (in-memory cache TTL in seconds), `APP_HTTP_CACHE_TTL` (HTTP Cache-Control max-age).
- Request validation and signing: upstream validation must be configured (routes use `validation.ProcessImageContextFromPath`).

## Path parameters

The path after `/videos/preview/` is parsed to extract transformation parameters and video URL:

Format: `/videos/preview/{params}/{base64-encoded-url-or-location}`

Supported parameters (can be combined):
- `q:{quality}` - JPEG/WebP quality (1-100, default varies)
- `w:{width}` - target width in pixels
- `h:{height}` - target height in pixels
- `s:{scale}` - scale factor (e.g., 0.5 for 50%, 2.0 for 200%)
- `webp` - convert to WebP format (default is JPEG)
- `f:{position}` - frame position: `first`, `middle`, or `last` (default is `first`)
- `loc:{location}` - explicit S3 location (requires signature)
- `i:{interpolation}` - interpolation method for resizing (e.g., `lanczos`, `linear`, `cubic`)

## Request details

### Video sources

1. **S3 location** (when `loc:{location}` parameter is present):
   - The handler validates the S3 object exists and is a video
   - Generates a presigned URL (1-hour expiration) for ffmpeg to access
   - Content-Type is validated from S3 metadata

2. **HTTP/HTTPS URL** (default):
   - The handler performs a HEAD request to validate content type
   - Must return a valid video MIME type
   - URL is passed directly to ffmpeg for frame extraction

### Frame extraction

- Uses ffmpeg to extract a single frame at the specified position
- Supported positions:
  - `first` - extracts the first frame (default)
  - `middle` - extracts a frame from the middle of the video
  - `last` - extracts the last frame
- Frame extraction is performed via the `extractFrameFromPosition` function

### Image transformations

Applied in order:
1. **Resize** (if `w:` or `h:` specified): resizes to exact dimensions
2. **Rescale** (if `s:` specified): scales by percentage
3. **Encode**: converts to WebP or JPEG with specified quality

## Caching behavior

1. **Memory cache check**: checks Ristretto cache first
2. **S3 cache check**: if memory cache misses, checks S3 cache
3. **Generate preview**: if both caches miss:
   - Validates video source (S3 or HTTP)
   - Extracts frame at specified position
   - Applies transformations (resize, rescale)
   - Encodes to target format (WebP or JPEG)
   - Stores in both memory and S3 caches (if enabled)

## Headers set

- `Content-Type`: `image/webp` or `image/jpeg` depending on output format
- `Cache-Control`: `public, max-age={APP_HTTP_CACHE_TTL}` for client-side caching

## Errors and status codes

- 200 OK — preview image generated and served successfully
- 403 Forbidden — invalid content type or video MIME type not allowed
- 404 Not Found — S3 object not found (when using S3 location)
- 500 Internal Server Error — failures during:
  - Content type validation
  - Frame extraction
  - Image processing (resize, rescale, encode)
  - S3 operations (stat, presigned URL generation)

## Examples

### Basic preview (first frame, default quality)

```bash
curl "http://localhost:3000/videos/preview/<base64-encoded-url>"
```

### Preview with specific dimensions and WebP format

```bash
# Extract first frame, resize to 500x300, convert to WebP at quality 80
curl "http://localhost:3000/videos/preview/w:500/h:300/q:80/webp/<base64-encoded-url>"
```

### Preview from middle of video with scaling

```bash
# Extract middle frame, scale to 50%, JPEG quality 90
curl "http://localhost:3000/videos/preview/f:middle/s:0.5/q:90/<base64-encoded-url>"
```

### Preview from S3 location

```bash
# Extract last frame from S3 object, resize to 800x600
curl "http://localhost:3000/videos/preview/loc:<signed-location>/f:last/w:800/h:600/<base64-encoded-url>"
```

### High-quality WebP thumbnail

```bash
# First frame, 300x200, WebP quality 95, Lanczos interpolation
curl "http://localhost:3000/videos/preview/w:300/h:200/q:95/webp/i:lanczos/<base64-encoded-url>"
```

## Performance considerations

- Frame extraction requires ffmpeg and may be CPU-intensive for large videos
- S3 presigned URLs are generated on-demand (1-hour expiration)
- Caching significantly improves performance for repeated preview requests
- Consider pre-generating previews for frequently accessed videos
- Memory and S3 caches work together to minimize redundant processing

## Integration with video upload

When uploading videos using the single or multi-part upload endpoints, you can generate previews by:

1. Upload video to S3 (single upload or multi-part)
2. Use the returned `location` in a preview request with `loc:{location}` parameter
3. Ensure the request is properly signed if required by your security configuration

## MIME type validation

Only video MIME types are accepted:
- video/mp4
- video/quicktime
- video/x-msvideo
- video/x-matroska
- video/webm
- And other standard video MIME types (validated via `validation.IsVideoMime`)
