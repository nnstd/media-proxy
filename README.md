# Media Proxy

## Optional S3 result storage

Set these environment variables to enable persistent result caching in S3/MinIO. If an object exists, it will be served directly without reprocessing.

- `S3_ENABLED` (bool) — enable S3 storage, default false
- `S3_ENDPOINT` — e.g. `play.min.io:9000` or your MinIO/S3 endpoint
- `S3_ACCESS_KEY_ID`
- `S3_SECRET_ACCESS_KEY`
- `S3_BUCKET` — target bucket
- `S3_SSL` (bool) — default true for S3, false for plain MinIO if needed
- `S3_PREFIX` — optional key prefix, e.g. `media-proxy/`

MinIO Go SDK is used under the hood. See the official docs: [minio/minio-go](https://github.com/minio/minio-go).

A high-performance media proxy service built with Go and Fiber that provides secure proxying for images and video preview generation. This service allows you to proxy media content from allowed origins while maintaining security and performance.

## Features

- **Image Proxying**: Proxy images from allowed origins with optional quality control, WebP conversion, resizing, and rescaling
- **WebP Conversion**: Convert any supported image format to WebP with quality optimization
- **Video Preview Generation**: Extract frame thumbnails from video files at specific positions (first, middle, last, or custom time)
- **Origin Validation**: Whitelist-based origin control for security
- **MIME Type Validation**: Strict content type checking for both images and videos
- **Health Checks**: Built-in health check endpoint
- **Compression**: Automatic response compression
- **Structured Logging**: JSON-formatted logging with Zap
- **Path-based Parameters**: Clean URL structure with parameters in the path
- **HMAC Signatures**: Optional URL signing for enhanced security
- **Image Upload**: Direct image upload with processing capabilities

## Supported Media Types

### Images
- JPEG (`image/jpeg`)
- PNG (`image/png`)
- WebP (`image/webp`)
- GIF (`image/gif`)
- BMP (`image/bmp`)
- TIFF (`image/tiff`)
- AVIF (`image/avif`)

### Documents
- PDF (`application/pdf`)
- EPUB (`application/epub+zip`)
- MOBI (`application/x-mobipocket-ebook`)
- DOCX (`application/vnd.openxmlformats-officedocument.wordprocessingml.document`)
- XLSX (`application/vnd.openxmlformats-officedocument.spreadsheetml.sheet`)
- PPTX (`application/vnd.openxmlformats-officedocument.presentationml.presentation`)

### Videos
- MP4 (`video/mp4`)
- OGG (`video/ogg`)
- WebM (`video/webm`)
- QuickTime (`video/quicktime`)
- AVI (`video/x-msvideo`)
- Matroska (`video/x-matroska`)
- FLV (`video/x-flv`)
- M4V (`video/x-m4v`)

## Installation

### Prerequisites
- Go 1.24 or later
- FFmpeg libraries (for video processing)

### Clone and Build
```bash
git clone <repository-url>
cd media-proxy
go mod download
go build -o media-proxy
```

### Docker
```bash
docker build -t media-proxy .
docker run -p 3000:3000 -e APP_ALLOWED_ORIGINS=example.com,cdn.example.com media-proxy
```

## Configuration

The service is configured via environment variables:

| Variable | Description | Required | Default |
|----------|-------------|----------|---------|
| `APP_ALLOWED_ORIGINS` | Comma-separated list of allowed hostnames | No | Empty (allows all) |
| `APP_ADDRESS` | Address to listen on | No | `:3000` |
| `APP_PREFORK` | Enable [preforking](https://docs.gofiber.io/api/fiber#config) | No | `false` |
| `APP_METRICS` | Enable metrics | No | `true` |
| `APP_WEBP` | Default to WebP conversion | No | `false` |
| `APP_CACHE_TTL_SECONDS` | Cache TTL in seconds | No | `1800` (30 minutes) |
| `APP_CACHE_MAX_COST` | Cache max cost in bytes | No | `1073741824` (1GB) |
| `APP_CACHE_NUM_COUNTERS` | Cache num counters | No | `10000000` (10M) |
| `APP_CACHE_BUFFER_ITEMS` | Cache buffer items | No | `64` |
| `APP_TOKEN` | Token for image upload authentication | No | Empty |
| `APP_HMAC_KEY` | HMAC key for URL signing | No | Empty |

### Example Configuration
```bash
export APP_ALLOWED_ORIGINS="example.com,cdn.example.com,media.example.org"
export APP_TOKEN="your-upload-token"
export APP_HMAC_KEY="your-hmac-secret-key"
```

## API Endpoints

### Health Check
```
GET /health
```
Returns the health status of the service.

### Image Proxy

#### New Path-based Format (Recommended)
```
GET /images/q:<quality>/w:<width>/h:<height>/s:<scale>/i:<interpolation>/webp/sig:<signature>/{base64-encoded-url}
```

**Path Parameters:**
- `q` or `quality`: Image quality for optimization (1-100, default: 100)
- `w` or `width`: Width of the image (default: 0)
- `h` or `height`: Height of the image (default: 0)
- `s` or `scale`: Scale factor for the image (0-1, default: 0)
- `i` or `interpolation`: Interpolation method for resizing (0-5, default: 5)
- `webp`: Force conversion to WebP format (flag, no value needed)
- `sig` or `signature`: HMAC signature for URL validation (optional)
- `{base64-encoded-url}`: Base64 URL-encoded image URL (required)

**Interpolation methods:**
- 0: Nearest-neighbor interpolation
- 1: Bilinear interpolation
- 2: Bicubic interpolation
- 3: Mitchell-Netravali interpolation
- 4: Lanczos2 interpolation
- 5: Lanczos3 interpolation

**Examples:**
```bash
# Basic image proxy (original format)
curl "http://localhost:3000/images/aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5qcGc="

# Convert to WebP with quality optimization
curl "http://localhost:3000/images/q:85/webp/aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5qcGc="

# Resize image with specific dimensions
curl "http://localhost:3000/images/w:800/h:600/q:90/aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5qcGc="

# Rescale image with custom interpolation
curl "http://localhost:3000/images/s:0.5/i:2/aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5qcGc="

# With HMAC signature for security
curl "http://localhost:3000/images/sig:abc123/q:75/webp/aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5qcGc="
```

#### Legacy Query-based Format (Backward Compatibility)
```
GET /image?url=<image_url>&quality=<1-100>&webp=<true|false>&width=<width>&height=<height>&scale=<scale>&interpolation=<interpolation>&signature=<signature>
```

**Parameters:**
- `url` (required): The URL of the image to proxy
- `quality` (optional): Image quality for optimization (1-100, default: 100)
- `webp` (optional): Force conversion to WebP format (true/false, default: false)
- `width` (optional): Width of the image (default: 0)
- `height` (optional): Height of the image (default: 0)
- `scale` (optional): Scale factor for the image (0-1, default: 0)
- `interpolation` (optional): Interpolation method for resizing (0-5, default: 5)
- `signature` (optional): HMAC signature for URL validation

**Examples:**
```bash
# Basic image proxy (original format)
curl "http://localhost:3000/image?url=https://example.com/image.jpg"

# Convert to WebP with quality optimization
curl "http://localhost:3000/image?url=https://example.com/image.jpg&webp=true&quality=85"

# Quality optimization without format conversion
curl "http://localhost:3000/image?url=https://example.com/image.jpg&quality=75"

# Rescale image
curl "http://localhost:3000/image?url=https://example.com/image.jpg&scale=0.5"

# Resize image
curl "http://localhost:3000/image?url=https://example.com/image.jpg&width=100&height=100"

# Convert PDF first page to image
curl "http://localhost:3000/image?url=https://example.com/document.pdf"

# Convert DOCX first page to WebP with resizing
curl "http://localhost:3000/image?url=https://example.com/document.docx&webp=true&width=800&height=600"

# Convert EPUB first page to thumbnail
curl "http://localhost:3000/image?url=https://example.com/book.epub&scale=0.3&quality=85"

# Convert PowerPoint first slide to image
curl "http://localhost:3000/image?url=https://example.com/presentation.pptx&width=1200&height=800"
```

### Image Upload

#### New Path-based Format (Recommended)
```
POST /images/upload/q:<quality>/w:<width>/h:<height>/s:<scale>/i:<interpolation>/webp/t:<token>/{base64-encoded-url}
```

**Path Parameters:**
- `q` or `quality`: Image quality for optimization (1-100, default: 100)
- `w` or `width`: Width of the image (default: 0)
- `h` or `height`: Height of the image (default: 0)
- `s` or `scale`: Scale factor for the image (0-1, default: 0)
- `i` or `interpolation`: Interpolation method for resizing (0-5, default: 5)
- `webp`: Force conversion to WebP format (flag, no value needed)
- `t` or `token`: Authentication token (required)
- `{base64-encoded-url}`: Base64 URL-encoded image URL (required)

**Request Body:** Raw image data

**Examples:**
```bash
# Upload and process image
curl -X POST "http://localhost:3000/images/upload/q:85/webp/t:your-token/aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5qcGc=" \
  --data-binary @image.jpg

# Upload and resize image
curl -X POST "http://localhost:3000/images/upload/w:800/h:600/t:your-token/aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5qcGc=" \
  --data-binary @image.jpg
```

#### Legacy Query-based Format (Backward Compatibility)
```
POST /images?url=<image_url>&quality=<1-100>&webp=<true|false>&width=<width>&height=<height>&scale=<scale>&interpolation=<interpolation>&token=<token>
```

**Parameters:** Same as legacy image proxy format, plus:
- `token` (required): Authentication token

**Request Body:** Raw image data

### Video Preview

#### Path-based Format
```
GET /videos/preview/q:<quality>/w:<width>/h:<height>/s:<scale>/i:<interpolation>/fp:<framePosition>/webp/sig:<signature>/{base64-encoded-url}
```

**Path Parameters:**
- `q` or `quality`: Image quality for optimization (1-100, default: 100)
- `w` or `width`: Width of the image (default: 0)
- `h` or `height`: Height of the image (default: 0)
- `s` or `scale`: Scale factor for the image (0-1, default: 0)
- `i` or `interpolation`: Interpolation method for resizing (0-5, default: 5)
- `fp` or `framePosition`: Frame position to extract (default: "first")
- `webp`: Force conversion to WebP format (flag, no value needed)
- `sig` or `signature`: HMAC signature for URL validation (optional)
- `{base64-encoded-url}`: Base64 URL-encoded video URL (required)

**Frame Position Options:**
- `first`: Extract the first frame (default)
- `half`: Extract a frame from the middle of the video
- `last`: Extract the last frame
- `30.5`: Extract a frame at 30.5 seconds (supports decimal seconds)

**Examples:**
```bash
# Basic video preview (first frame)
curl "http://localhost:3000/videos/preview/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ="

# Video preview from the middle of the video
curl "http://localhost:3000/videos/preview/fp:half/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ="

# Video preview from the last frame
curl "http://localhost:3000/videos/preview/fp:last/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ="

# Video preview from 30 seconds into the video
curl "http://localhost:3000/videos/preview/fp:30/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ="

# Video preview from the middle with resizing and WebP conversion
curl "http://localhost:3000/videos/preview/fp:half/w:320/h:240/q:85/webp/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ="
```

**Response:**
- Returns a JPEG or WebP thumbnail of the extracted frame
- Content-Type: `image/jpeg` or `image/webp` (if WebP conversion is enabled)
- Cache-Control: `public, max-age=3600`
- Validates that the URL origin is in the allowed list
- Validates that the content type is a supported video format

### Video Proxy

#### Path-based Format
```
GET /videos/sig:<signature>/{base64-encoded-url}
```

**Path Parameters:**
- `sig` or `signature`: HMAC signature for URL validation (optional for HTTP/HTTPS origins, required for S3 explicit locations)
- `{base64-encoded-url}`: Base64 URL-encoded video URL or S3 location (required)

**Features:**
- Supports HTTP Range requests for video streaming (partial content)
- Proxies raw video bytes from HTTP/HTTPS origins
- Supports proxying from S3/MinIO storage (if explicit location provided)
- Forwards relevant headers (Content-Type, Accept-Ranges, Content-Length, Content-Range)
- Returns appropriate HTTP status codes (200 OK or 206 Partial Content)

**Examples:**
```bash
# Proxy video from HTTP origin
curl "http://localhost:3000/videos/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ=" \
  -o video.mp4

# Proxy video with Range request (first 1MB)
curl "http://localhost:3000/videos/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ=" \
  -H "Range: bytes=0-1048575" \
  -o video_partial.mp4

# Proxy video from S3 location (requires signature)
curl "http://localhost:3000/videos/sig:abc123/aHR0cHM6Ly9zMy5hbWF6b25hd3MuY29tL2J1Y2tldC92aWRlby5tcDQ=" \
  -o video.mp4
```

**Response:**
- Returns raw video bytes
- Content-Type: Original video content type (e.g., `video/mp4`)
- Accept-Ranges: `bytes`
- Supports Content-Range header for partial content responses (206 Partial Content)
- Validates that the URL origin is in the allowed list (for HTTP/HTTPS origins)
- Validates HMAC signature for S3 explicit locations

## URL Encoding for Path-based Format

For the new path-based format, you need to base64 URL-encode your image/video URLs:

```bash
# Example URL encoding
echo -n "https://example.com/image.jpg" | base64 -w 0 | sed 's/+/-/g' | sed 's/\//_/g' | sed 's/=//g'
# Result: aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5qcGc
```

## HMAC Signature Generation

To generate HMAC signatures for secure URL validation:

```bash
# Using OpenSSL
echo -n "https://example.com/image.jpg" | openssl dgst -sha256 -hmac "your-hmac-key" -binary | xxd -p

# Using Python
python3 -c "
import hmac
import hashlib
url = 'https://example.com/image.jpg'
key = 'your-hmac-key'
signature = hmac.new(key.encode(), url.encode(), hashlib.sha256).hexdigest()
print(signature)
"
```

## Usage Examples

### Basic Image Proxying
```bash
# Path-based format (recommended)
curl "http://localhost:3000/images/q:85/webp/aHR0cHM6Ly9leGFtcGxlLmNvbS9waG90by5qcGc="

# Legacy query-based format
curl "http://localhost:3000/image?url=https://example.com/photo.jpg&webp=true&quality=85"
```

### Video Preview Generation
```bash
# Path-based format - first frame
curl "http://localhost:3000/videos/preview/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ=" \
  -H "Accept: image/jpeg" \
  -o thumbnail.jpg

# Path-based format - frame from middle of video
curl "http://localhost:3000/videos/preview/fp:half/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ=" \
  -H "Accept: image/jpeg" \
  -o thumbnail.jpg
```

### Video Proxying
```bash
# Proxy full video
curl "http://localhost:3000/videos/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ=" \
  -o video.mp4

# Proxy video with Range request
curl "http://localhost:3000/videos/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ=" \
  -H "Range: bytes=0-1048575" \
  -o video_partial.mp4
```

### Image Upload
```bash
# Path-based format (recommended)
curl -X POST "http://localhost:3000/images/upload/q:90/webp/t:your-token/aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5qcGc=" \
  --data-binary @image.jpg

# Legacy query-based format
curl -X POST "http://localhost:3000/images?url=https://example.com/image.jpg&webp=true&quality=90&token=your-token" \
  --data-binary @image.jpg
```

### HTML Integration
```html
<!-- Path-based format (recommended) -->
<img src="http://localhost:3000/images/q:80/webp/aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5qcGc=" 
     alt="WebP optimized image">

<!-- Legacy query-based format -->
<img src="http://localhost:3000/image?url=https%3A//example.com/image.jpg&webp=true&quality=80" 
     alt="WebP optimized image">

<!-- Video thumbnail -->
<img src="http://localhost:3000/videos/preview/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ=" 
     alt="Video thumbnail">

<!-- Video thumbnail from middle of video -->
<img src="http://localhost:3000/videos/preview/fp:half/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ=" 
     alt="Video thumbnail from middle">

<!-- Video proxy for HTML5 video player -->
<video controls>
  <source src="http://localhost:3000/videos/aHR0cHM6Ly9leGFtcGxlLmNvbS92aWRlby5tcDQ=" type="video/mp4">
</video>
```

## Security Features

1. **Origin Validation**: Only URLs from configured allowed origins are processed
2. **MIME Type Validation**: Strict content type checking prevents processing of non-media files
3. **URL Parsing**: Robust URL validation prevents malformed requests
4. **No Direct File Access**: Service only processes HTTP/HTTPS URLs
5. **HMAC Signatures**: Optional URL signing for enhanced security
6. **Token Authentication**: Required authentication for image upload operations

## Development

### Running Locally
```bash
# Set environment variables
export APP_ALLOWED_ORIGINS="localhost,127.0.0.1,example.com"
export APP_TOKEN="your-upload-token"
export APP_HMAC_KEY="your-hmac-secret-key"

# Run the service
go run .
```

The service will start on port 3000.

### Testing
```bash
# Test path-based image proxy
curl "http://localhost:3000/images/aHR0cHM6Ly9odHRwYmluLm9yZy9pbWFnZS9qcGVn"

# Test legacy query-based image proxy
curl "http://localhost:3000/image?url=https://httpbin.org/image/jpeg"

# Test path-based video preview (first frame)
curl "http://localhost:3000/videos/preview/aHR0cHM6Ly9leGFtcGxlLmNvbS9zYW1wbGUubXA0"

# Test path-based video preview (middle frame)
curl "http://localhost:3000/videos/preview/fp:half/aHR0cHM6Ly9leGFtcGxlLmNvbS9zYW1wbGUubXA0"

# Test video proxy
curl "http://localhost:3000/videos/aHR0cHM6Ly9leGFtcGxlLmNvbS9zYW1wbGUubXA0" \
  -o test_video.mp4

# Test video proxy with Range request
curl "http://localhost:3000/videos/aHR0cHM6Ly9leGFtcGxlLmNvbS9zYW1wbGUubXA0" \
  -H "Range: bytes=0-1048575" \
  -o test_video_partial.mp4

# Test health check
curl "http://localhost:3000/health"
```

## License

Media-server is licensed under the [GNU Affero General Public License v3.0](https://github.com/nnstd/media-proxy/blob/main/LICENSE) and as commercial software. For commercial licensing, please contact us at sales@nnstd.dev.
