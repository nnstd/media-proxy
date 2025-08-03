package routes

import (
	"strconv"
	"strings"

	"media-proxy/validation"
)

type CacheValue struct {
	Body        []byte
	ContentType string
}

func cacheKey(url string, params *validation.ImageContext) string {
	// Use string builder for more efficient cache key generation
	var builder strings.Builder
	builder.WriteString(url)
	builder.WriteString(";quality=")
	builder.WriteString(strconv.Itoa(params.Quality))
	builder.WriteString(";width=")
	builder.WriteString(strconv.Itoa(params.Width))
	builder.WriteString(";height=")
	builder.WriteString(strconv.Itoa(params.Height))
	builder.WriteString(";scale=")
	builder.WriteString(strconv.FormatFloat(params.Scale, 'f', -1, 64))
	builder.WriteString(";interpolation=")
	builder.WriteString(strconv.Itoa(int(params.Interpolation)))
	builder.WriteString(";webp=")
	builder.WriteString(strconv.FormatBool(params.Webp))
	return builder.String()
}
