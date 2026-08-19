package locales

import (
	"bbs-go/internal/pkg/config"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/spf13/viper"
)

const (
	HeaderAppLanguage = "X-App-Language"
	HeaderLanguage    = "X-Language"
)

type messageMatcher struct {
	key     string
	pattern *regexp.Regexp
}

var (
	mu             sync.RWMutex
	viperInstances = make(map[string]*viper.Viper)
	exactMessages  = make(map[string]string)
	formatMessages []messageMatcher
)

func getLocaleDir() string {
	workDir, err := os.Executable()
	if err != nil {
		return "./locales"
	}
	localeDir := filepath.Join(filepath.Dir(workDir), "locales")
	if _, err := os.Stat(localeDir); err == nil {
		return localeDir
	}
	return "./locales"
}

func Init() error {
	return initFromDir(getLocaleDir())
}

func initFromDir(localeDir string) error {
	files, err := filepath.Glob(filepath.Join(localeDir, "*.yml"))
	if err != nil {
		return fmt.Errorf("failed to find locale files: %w", err)
	}
	if len(files) == 0 {
		return fmt.Errorf("no locale files found in %s", localeDir)
	}

	instances := make(map[string]*viper.Viper, len(files))
	for _, file := range files {
		v := viper.New()
		v.SetConfigFile(file)
		v.SetConfigType("yaml")
		if err := v.ReadInConfig(); err != nil {
			return fmt.Errorf("failed to read locale file %s: %w", file, err)
		}
		locale := Normalize(getLocaleByFile(file))
		if locale == "" {
			continue
		}
		instances[locale] = v
	}

	exact, matchers := buildMessageIndex(instances)
	mu.Lock()
	viperInstances = instances
	exactMessages = exact
	formatMessages = matchers
	mu.Unlock()
	return nil
}

func getLocaleByFile(file string) string {
	base := filepath.Base(file)
	if strings.HasSuffix(base, ".yml") {
		return strings.TrimSuffix(base, ".yml")
	}
	if strings.HasSuffix(base, ".yaml") {
		return strings.TrimSuffix(base, ".yaml")
	}
	return ""
}

func Normalize(language string) string {
	language = strings.TrimSpace(strings.ReplaceAll(language, "_", "-"))
	if i := strings.IndexByte(language, ';'); i >= 0 {
		language = language[:i]
	}
	switch strings.ToLower(strings.TrimSpace(language)) {
	case "zh", "zh-cn", "zh-hans", "cn":
		return string(config.LanguageZhCN)
	case "en", "en-us", "en-gb":
		return string(config.LanguageEnUS)
	case "my", "my-mm", "mm", "burmese":
		return string(config.LanguageMyMM)
	default:
		return ""
	}
}

func Default() string {
	if config.Instance != nil {
		if locale := Normalize(string(config.Instance.Language)); locale != "" {
			return locale
		}
	}
	return string(config.DefaultLanguage)
}

func IsSupported(locale string) bool {
	locale = Normalize(locale)
	mu.RLock()
	_, ok := viperInstances[locale]
	mu.RUnlock()
	return ok
}

func Resolve(raw string) string {
	for _, item := range strings.Split(raw, ",") {
		if locale := Normalize(item); locale != "" && IsSupported(locale) {
			return locale
		}
	}
	fallback := Default()
	if IsSupported(fallback) {
		return fallback
	}
	if IsSupported(string(config.LanguageEnUS)) {
		return string(config.LanguageEnUS)
	}
	return fallback
}

func RequestLocale(r *http.Request) string {
	if r == nil {
		return Resolve("")
	}
	if value := r.Header.Get(HeaderAppLanguage); strings.TrimSpace(value) != "" {
		return Resolve(value)
	}
	if value := r.Header.Get(HeaderLanguage); strings.TrimSpace(value) != "" {
		return Resolve(value)
	}
	return Resolve(r.Header.Get("Accept-Language"))
}

func Get(key string) string {
	return GetFor(Default(), key)
}

func GetFor(locale, key string) string {
	locale = Resolve(locale)
	mu.RLock()
	v := viperInstances[locale]
	fallback := viperInstances[Default()]
	english := viperInstances[string(config.LanguageEnUS)]
	mu.RUnlock()

	if v != nil && v.IsSet(key) {
		return v.GetString(key)
	}
	if fallback != nil && fallback.IsSet(key) {
		slog.Warn("translation key missing in requested locale", "key", key, "locale", locale)
		return fallback.GetString(key)
	}
	if english != nil && english.IsSet(key) {
		return english.GetString(key)
	}
	slog.Warn("translation key not found", "key", key, "locale", locale)
	return key
}

func Getf(key string, args ...any) string {
	return fmt.Sprintf(Get(key), args...)
}

func GetfFor(locale, key string, args ...any) string {
	return fmt.Sprintf(GetFor(locale, key), args...)
}

// TranslateMessage translates a backend-generated message that was created
// using another loaded locale. User content is never passed through this
// function automatically.
func TranslateMessage(message, targetLocale string) string {
	if message == "" {
		return message
	}
	targetLocale = Resolve(targetLocale)

	mu.RLock()
	key, ok := exactMessages[message]
	matchers := append([]messageMatcher(nil), formatMessages...)
	mu.RUnlock()
	if ok {
		return GetFor(targetLocale, key)
	}
	for _, matcher := range matchers {
		matches := matcher.pattern.FindStringSubmatch(message)
		if matches == nil {
			continue
		}
		return interpolateCaptured(GetFor(targetLocale, matcher.key), matches[1:])
	}
	return message
}

func buildMessageIndex(instances map[string]*viper.Viper) (map[string]string, []messageMatcher) {
	exact := make(map[string]string)
	var matchers []messageMatcher
	locales := make([]string, 0, len(instances))
	for locale := range instances {
		locales = append(locales, locale)
	}
	sort.Strings(locales)
	for _, locale := range locales {
		v := instances[locale]
		for _, key := range v.AllKeys() {
			value := v.GetString(key)
			if value == "" {
				continue
			}
			if strings.Contains(value, "%") {
				if pattern := compileFormatPattern(value); pattern != nil {
					matchers = append(matchers, messageMatcher{key: key, pattern: pattern})
				}
				continue
			}
			if _, exists := exact[value]; !exists {
				exact[value] = key
			}
		}
	}
	return exact, matchers
}

func compileFormatPattern(format string) *regexp.Regexp {
	var b strings.Builder
	b.WriteByte('^')
	captures := 0
	for i := 0; i < len(format); {
		if format[i] == '%' && i+1 < len(format) {
			next := format[i+1]
			if next == '%' {
				b.WriteString("%")
				i += 2
				continue
			}
			if next == 'd' {
				b.WriteString(`([-+]?\d+)`)
				captures++
				i += 2
				continue
			}
			if next == 's' || next == 'v' {
				b.WriteString(`(.+?)`)
				captures++
				i += 2
				continue
			}
		}
		b.WriteString(regexp.QuoteMeta(string(format[i])))
		i++
	}
	b.WriteByte('$')
	if captures == 0 {
		return nil
	}
	pattern, err := regexp.Compile(b.String())
	if err != nil {
		slog.Warn("failed to compile locale format pattern", "format", format, "error", err)
		return nil
	}
	return pattern
}

func interpolateCaptured(format string, args []string) string {
	var b strings.Builder
	argIndex := 0
	for i := 0; i < len(format); {
		if format[i] == '%' && i+1 < len(format) {
			next := format[i+1]
			if next == '%' {
				b.WriteByte('%')
				i += 2
				continue
			}
			if (next == 'd' || next == 's' || next == 'v') && argIndex < len(args) {
				b.WriteString(args[argIndex])
				argIndex++
				i += 2
				continue
			}
		}
		b.WriteByte(format[i])
		i++
	}
	return b.String()
}
