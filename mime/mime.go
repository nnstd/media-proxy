package mime

var imageMimeTypes = []string{
	"image/jpeg",
	"image/png",
	"image/webp",
	"image/gif",
	"image/bmp",
	"image/tiff",
	"image/avif",
}

var videoMimeTypes = []string{
	"video/mp4",
	"video/ogg",
	"video/webm",
	"video/quicktime",
	"video/x-msvideo",
	"video/x-matroska",
	"video/x-flv",
	"video/x-m4v",
	"video/x-m4v",
}

func IsImageMime(mimeType string) bool {
	for _, imageMimeType := range imageMimeTypes {
		if mimeType == imageMimeType {
			return true
		}
	}

	return false
}

func IsVideoMime(mimeType string) bool {
	for _, videoMimeType := range videoMimeTypes {
		if mimeType == videoMimeType {
			return true
		}
	}

	return false
}
