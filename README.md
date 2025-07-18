# Media Proxy

A high-performance media proxy service built with Go and Fiber that provides secure proxying for images and video preview generation. This service allows you to proxy media content from allowed origins while maintaining security and performance.

## Features

- **Image Proxying**: Proxy images from allowed origins with optional quality control, WebP conversion, resizing, and rescaling
- **WebP Conversion**: Convert any supported image format to WebP with quality optimization
- **Video Preview Generation**: Extract first frame thumbnails from video files
- **Origin Validation**: Whitelist-based origin control for security
- **MIME Type Validation**: Strict content type checking for both images and videos
- **Health Checks**: Built-in health check endpoint
- **Compression**: Automatic response compression
- **Structured Logging**: JSON-formatted logging with Zap

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

### Example Configuration
```bash
export APP_ALLOWED_ORIGINS="example.com,cdn.example.com,media.example.org"
```

## API Endpoints

### Health Check
```
GET /health
```
Returns the health status of the service.

### Image Proxy
```
GET /image?url=<image_url>&quality=<1-100>&webp=<true|false>&width=<width>&height=<height>&scale=<scale>&interpolation=<interpolation>
```

**Parameters:**
- `url` (required): The URL of the image to proxy
- `quality` (optional): Image quality for optimization (1-100, default: 100)
- `webp` (optional): Force conversion to WebP format (true/false, default: false)
- `width` (optional): Width of the image (default: 0)
- `height` (optional): Height of the image (default: 0)
- `scale` (optional): Scale factor for the image (0-1, default: 0)
- `interpolation` (optional): Interpolation method for resizing (0-5, default: 5)

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

**Response:**
- Returns the proxied image (original format or WebP if requested)
- For documents: Returns first page/slide as an image
- Content-Type: Original format or `image/webp` when converted
- Validates that the URL origin is in the allowed list
- Validates that the content type is a supported image or document format
- Automatic format conversion when quality < 100 or webp=true

### Video Preview
```
GET /video/preview?url=<video_url>
```

**Parameters:**
- `url` (required): The URL of the video to generate a preview from

**Example:**
```bash
curl "http://localhost:3000/video/preview?url=https://example.com/video.mp4" -o preview.jpg
```

**Response:**
- Returns a JPEG thumbnail of the first frame
- Content-Type: `image/jpeg`
- Cache-Control: `public, max-age=3600`
- Validates that the URL origin is in the allowed list
- Validates that the content type is a supported video format

## Usage Examples

### Basic Image Proxying
```bash
# Proxy an image with default quality (original format)
curl "http://localhost:3000/image?url=https://example.com/photo.jpg"

# Convert any image to WebP with quality optimization
curl "http://localhost:3000/image?url=https://example.com/photo.jpg&webp=true&quality=85"

# Reduce quality without format conversion (triggers processing)
curl "http://localhost:3000/image?url=https://example.com/photo.jpg&quality=75"

# Force WebP conversion without quality change
curl "http://localhost:3000/image?url=https://example.com/photo.png&webp=true"
```

### Video Preview Generation
```bash
# Generate a thumbnail from a video
curl "http://localhost:3000/video/preview?url=https://example.com/video.mp4" \
  -H "Accept: image/jpeg" \
  -o thumbnail.jpg
```

### HTML Integration
```html
<!-- Proxied image (original format) -->
<img src="http://localhost:3000/image?url=https%3A//example.com/image.jpg" 
     alt="Proxied image">

<!-- WebP optimized image -->
<img src="http://localhost:3000/image?url=https%3A//example.com/image.jpg&webp=true&quality=80" 
     alt="WebP optimized image">

<!-- Video thumbnail -->
<img src="http://localhost:3000/video/preview?url=https%3A//example.com/video.mp4" 
     alt="Video thumbnail">
```

## Security Features

1. **Origin Validation**: Only URLs from configured allowed origins are processed
2. **MIME Type Validation**: Strict content type checking prevents processing of non-media files
3. **URL Parsing**: Robust URL validation prevents malformed requests
4. **No Direct File Access**: Service only processes HTTP/HTTPS URLs

## Development

### Running Locally
```bash
# Set environment variables
export APP_ALLOWED_ORIGINS="localhost,127.0.0.1,example.com"

# Run the service
go run .
```

The service will start on port 3000.

### Testing
```bash
# Test image proxy
curl "http://localhost:3000/image?url=https://httpbin.org/image/jpeg"

# Test video preview (requires a valid video URL)
curl "http://localhost:3000/video/preview?url=https://example.com/sample.mp4"

# Test health check
curl "http://localhost:3000/health"
```

## Dependencies

- [fiber](https://github.com/gofiber/fiber) - Web framework
- [zap](https://github.com/uber-go/zap) - Structured logging
- [go-astiav](https://github.com/asticode/go-astiav) - FFmpeg bindings for video processing
- [env](https://github.com/caarlos0/env) - Environment variable parsing
- [go-webp](https://github.com/kolesa-team/go-webp) - WebP image processing

## License

MIT
