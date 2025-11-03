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
| `APP_UPLOADING_ENABLED` | Enable video uploading to S3 | No | `false` |
| `APP_CHUNK_SIZE` | Chunk size for multi-part uploads (bytes) | No | `83886080` (80MB) |
| `REDIS_ENABLED` | Enable Redis for multi-part upload tracking | No | `false` |
| `REDIS_ADDR` | Redis server address | No | `localhost:6379` |
| `REDIS_PASSWORD` | Redis password | No | Empty |
| `REDIS_DB` | Redis database number | No | `0` |

### Example Configuration
```bash
export APP_ALLOWED_ORIGINS="example.com,cdn.example.com,media.example.org"
export APP_TOKEN="your-upload-token"
export APP_HMAC_KEY="your-hmac-secret-key"
export APP_UPLOADING_ENABLED=true  # Enable video uploads

# Optional: Redis for multi-part upload tracking
export REDIS_ENABLED=true
export REDIS_ADDR="localhost:6379"
export REDIS_PASSWORD=""
export REDIS_DB=0
export APP_CHUNK_SIZE=83886080  # 80MB chunks
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

### Video Upload

Upload videos directly to S3 storage with secure signature-based authentication.

#### Configuration
```bash
export APP_UPLOADING_ENABLED=true
export APP_HMAC_KEY="your-secret-hmac-key"
export S3_ENABLED=true
export S3_ENDPOINT="s3.amazonaws.com"
export S3_ACCESS_KEY_ID="your-access-key"
export S3_SECRET_ACCESS_KEY="your-secret-key"
export S3_BUCKET="your-bucket"
export S3_SSL=true
export APP_MAX_VIDEO_SIZE_MB=100
```

#### Endpoint
```
POST /videos?deadline={unix_timestamp}&location={base64_location}&signature={hmac_signature}
```

**Query Parameters:**
- `deadline`: Unix timestamp when the upload permission expires (required)
- `location`: Base64 URL-safe encoded S3 object key where the video will be stored (required)
- `signature`: HMAC-SHA256 signature of `deadline|location` (required)

**Form Data:**
- `video`: The video file to upload (required)

**Response (201 Created):**
```json
{
  "success": true,
  "location": "videos/user123/my-video.mp4",
  "size": 1048576
}
```

#### Generate Upload Signature (Node.js)
```javascript
const crypto = require('crypto');

function generateVideoUploadSignature(hmacKey, deadline, location) {
    // Create message: deadline|location
    const message = `${deadline}|${location}`;
    
    // Generate HMAC-SHA256 signature
    const hmac = crypto.createHmac('sha256', hmacKey);
    hmac.update(message);
    const signature = hmac.digest('hex');
    
    // Encode location in base64 URL-safe format
    const locationEncoded = Buffer.from(location)
        .toString('base64')
        .replace(/\+/g, '-')
        .replace(/\//g, '_')
        .replace(/=/g, '');
    
    return { signature, locationEncoded, deadline };
}

// Usage
const hmacKey = process.env.APP_HMAC_KEY;
const deadline = Math.floor(Date.now() / 1000) + (5 * 60); // 5 minutes
const location = 'videos/user123/my-video.mp4';

const { signature, locationEncoded } = generateVideoUploadSignature(
    hmacKey,
    deadline,
    location
);

console.log(`deadline=${deadline}`);
console.log(`location=${locationEncoded}`);
console.log(`signature=${signature}`);
```

#### Upload Video Example
```bash
# Using cURL
curl -X POST \
  "http://localhost:3000/videos?deadline=1730678400&location=dmlkZW9zL3VzZXIxMjMvbXktdmlkZW8ubXA0&signature=abc123..." \
  -F "video=@/path/to/video.mp4"
```

```javascript
// Using JavaScript/Fetch API
const formData = new FormData();
formData.append('video', videoFile);

const uploadUrl = new URL('http://localhost:3000/videos');
uploadUrl.searchParams.set('deadline', deadline);
uploadUrl.searchParams.set('location', locationEncoded);
uploadUrl.searchParams.set('signature', signature);

const response = await fetch(uploadUrl, {
    method: 'POST',
    body: formData
});

const result = await response.json();
console.log('Upload result:', result);
// Output: { success: true, location: "videos/user123/my-video.mp4", size: 1048576 }
```

#### Security Features
- **Deadline-based expiration**: Upload permissions expire after the specified deadline
- **HMAC signature validation**: Prevents unauthorized uploads
- **S3 location control**: Videos are uploaded to predetermined S3 paths
- **MIME type validation**: Only video files are accepted
- **File size limits**: Configure `APP_MAX_VIDEO_SIZE_MB` to prevent large uploads
- **Path sanitization**: Prevents directory traversal and invalid characters

For more details, see [VIDEO_UPLOAD.md](VIDEO_UPLOAD.md).

### Multi-Part Video Upload

For large video files, the service supports multi-part uploads with progress tracking via Redis.

#### Configuration
```bash
export APP_UPLOADING_ENABLED=true
export APP_TOKEN="your-upload-token"
export REDIS_ENABLED=true
export REDIS_ADDR="localhost:6379"
export S3_ENABLED=true
export S3_ENDPOINT="s3.amazonaws.com"
export S3_ACCESS_KEY_ID="your-access-key"
export S3_SECRET_ACCESS_KEY="your-secret-key"
export S3_BUCKET="your-bucket"
export APP_CHUNK_SIZE=83886080  # 80MB chunks
```

#### Step 1: Initialize Multi-Part Upload
```
POST /videos/multiparts?token={token}&deadline={unix_timestamp}&location={location}&size={bytes}&contentType={mime_type}&chunkSize={bytes}
```

**Query Parameters:**
- `token`: Authentication token (required)
- `deadline`: Unix timestamp when upload expires (required)
- `location`: S3 object key where video will be stored (required)
- `size`: Total file size in bytes (required)
- `contentType`: Video MIME type (required)
- `chunkSize`: Optional chunk size in bytes (default: 80MB)

**Response (200 OK):**
```json
{
  "uploadId": "1234567890-videos/user123/video.mp4",
  "location": "videos/user123/video.mp4",
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

#### Step 2: Upload Each Part
```
POST /videos/multiparts/{uploadId}/parts/{partIndex}?token={token}
```

**Path Parameters:**
- `uploadId`: Upload ID from initialization (required)
- `partIndex`: Zero-based part index (required)

**Query Parameters:**
- `token`: Authentication token (required)

**Form Data:**
- `video`: The video part data (required)

**Response (200 OK):**
```json
{
  "success": true,
  "uploadId": "1234567890-videos/user123/video.mp4",
  "partIndex": 0,
  "size": 83886080,
  "complete": false
}
```

When the last part is uploaded:
```json
{
  "success": true,
  "uploadId": "1234567890-videos/user123/video.mp4",
  "partIndex": 1,
  "size": 73400320,
  "complete": true,
  "message": "upload complete, parts will be merged"
}
```

#### Step 3: Check Upload Status
```
GET /videos/multiparts/{uploadId}?token={token}
```

**Path Parameters:**
- `uploadId`: Upload ID from initialization (required)

**Query Parameters:**
- `token`: Authentication token (required)

**Response (200 OK):**
```json
{
  "uploadId": "1234567890-videos/user123/video.mp4",
  "location": "videos/user123/video.mp4",
  "totalSize": 157286400,
  "partsCount": 2,
  "uploadedParts": [0, 1],
  "uploadedCount": 2,
  "complete": true,
  "progress": 100,
  "contentType": "video/mp4",
  "createdAt": "2025-11-03T11:00:00Z",
  "expiresAt": "2025-11-03T12:00:00Z"
}
```

#### Example: Multi-Part Upload (Node.js)
```javascript
const fs = require('fs');
const FormData = require('form-data');

const CHUNK_SIZE = 80 * 1024 * 1024; // 80MB
const token = 'your-upload-token';
const baseUrl = 'http://localhost:3000';

async function uploadVideoMultipart(filePath, location) {
  // Get file stats
  const stats = fs.statSync(filePath);
  const fileSize = stats.size;
  const contentType = 'video/mp4';
  
  // Calculate deadline (5 hours from now)
  const deadline = Math.floor(Date.now() / 1000) + (5 * 3600);
  
  // Step 1: Initialize upload
  const initUrl = new URL(`${baseUrl}/videos/multiparts`);
  initUrl.searchParams.set('token', token);
  initUrl.searchParams.set('deadline', deadline);
  initUrl.searchParams.set('location', location);
  initUrl.searchParams.set('size', fileSize);
  initUrl.searchParams.set('contentType', contentType);
  initUrl.searchParams.set('chunkSize', CHUNK_SIZE);
  
  const initResponse = await fetch(initUrl, { method: 'POST' });
  const uploadInfo = await initResponse.json();
  
  console.log(`Upload initialized: ${uploadInfo.uploadId}`);
  console.log(`Total parts: ${uploadInfo.partsCount}`);
  
  // Step 2: Upload each part
  const fileStream = fs.createReadStream(filePath);
  
  for (const part of uploadInfo.parts) {
    console.log(`Uploading part ${part.index + 1}/${uploadInfo.partsCount}...`);
    
    // Read chunk from file
    const buffer = Buffer.alloc(part.size);
    const fd = fs.openSync(filePath, 'r');
    fs.readSync(fd, buffer, 0, part.size, part.offset);
    fs.closeSync(fd);
    
    // Create form data
    const formData = new FormData();
    formData.append('video', buffer, { filename: 'part.mp4' });
    
    // Upload part
    const uploadUrl = new URL(`${baseUrl}/videos/multiparts/${uploadInfo.uploadId}/parts/${part.index}`);
    uploadUrl.searchParams.set('token', token);
    
    const partResponse = await fetch(uploadUrl, {
      method: 'POST',
      body: formData
    });
    
    const result = await partResponse.json();
    console.log(`Part ${part.index} uploaded (${result.complete ? 'COMPLETE' : 'in progress'})`);
  }
  
  // Step 3: Check final status
  const statusUrl = new URL(`${baseUrl}/videos/multiparts/${uploadInfo.uploadId}`);
  statusUrl.searchParams.set('token', token);
  
  const statusResponse = await fetch(statusUrl);
  const status = await statusResponse.json();
  
  console.log(`Upload complete: ${status.complete}`);
  console.log(`Progress: ${status.progress}%`);
  
  return status;
}

// Usage
uploadVideoMultipart('./large-video.mp4', 'videos/user123/large-video.mp4')
  .then(status => console.log('Done!', status))
  .catch(err => console.error('Error:', err));
```

#### Benefits of Multi-Part Upload
- **Resume capability**: Track which parts have been uploaded
- **Parallel uploads**: Upload multiple parts simultaneously
- **Progress tracking**: Monitor upload progress in real-time
- **Large file support**: Handle files larger than server memory limits
- **Fault tolerance**: Retry individual parts on failure

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
```

### Video Upload
```bash
# Generate signature (Node.js example)
node -e "
const crypto = require('crypto');
const hmacKey = 'your-secret-hmac-key';
const deadline = Math.floor(Date.now() / 1000) + 300; // 5 minutes
const location = 'videos/user123/my-video.mp4';
const message = \`\${deadline}|\${location}\`;
const signature = crypto.createHmac('sha256', hmacKey).update(message).digest('hex');
const locationEncoded = Buffer.from(location).toString('base64').replace(/\+/g, '-').replace(/\//g, '_').replace(/=/g, '');
console.log(\`deadline=\${deadline}&location=\${locationEncoded}&signature=\${signature}\`);
"

# Upload video using generated parameters
curl -X POST \
  "http://localhost:3000/videos?deadline=1730678400&location=dmlkZW9zL3VzZXIxMjMvbXktdmlkZW8ubXA0&signature=abc123..." \
  -F "video=@video.mp4"
```

### HTML Integration
```html
<!-- Path-based format (recommended) -->
<img src="http://localhost:3000/images/q:80/webp/aHR0cHM6Ly9leGFtcGxlLmNvbS9pbWFnZS5qcGc=" 
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
