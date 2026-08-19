package cache

import (
	"os"
	"strconv"
	"strings"

	goburrow "github.com/goburrow/cache"
)

func key2Int64(key goburrow.Key) int64 {
	return key.(int64)
}

// envInt keeps hot-cache sizing configurable without depending on the runtime
// YAML config. Cache packages are initialized before the application config is
// loaded, so environment variables are the safest way to tune a single-node
// deployment. Invalid or out-of-range values fall back to the supplied default.
func envInt(name string, defaultValue, minValue, maxValue int) int {
	raw := strings.TrimSpace(os.Getenv(name))
	if raw == "" {
		return defaultValue
	}
	value, err := strconv.Atoi(raw)
	if err != nil || value < minValue || value > maxValue {
		return defaultValue
	}
	return value
}
