package routes

import "fmt"

type CacheValue struct {
	Body        []byte
	ContentType string
}

func cacheKey(url string, params *imageContext) string {
	return fmt.Sprintf("%s;%s", url, params.String())
}
