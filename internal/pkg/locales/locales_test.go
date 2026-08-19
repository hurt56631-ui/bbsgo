package locales

import (
	"bbs-go/internal/pkg/config"
	"net/http/httptest"
	"path/filepath"
	"runtime"
	"testing"
)

func loadTestLocales(t *testing.T) {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate locales test file")
	}
	localeDir := filepath.Clean(filepath.Join(filepath.Dir(file), "../../../locales"))
	if err := initFromDir(localeDir); err != nil {
		t.Fatalf("init locales: %v", err)
	}
}

func TestNormalize(t *testing.T) {
	tests := map[string]string{
		"zh":           "zh-CN",
		"zh_Hans":      "zh-CN",
		"en-GB":        "en-US",
		"my":           "my-MM",
		"my_MM":        "my-MM",
		"my-MM;q=0.9":  "my-MM",
		"burmese":      "my-MM",
		"unsupported":  "",
		"  my-MM  ":    "my-MM",
		"en-US;q=0.8":  "en-US",
		"zh-CN; q=1.0": "zh-CN",
	}
	for input, want := range tests {
		if got := Normalize(input); got != want {
			t.Errorf("Normalize(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestRequestLocalePrefersAppHeader(t *testing.T) {
	loadTestLocales(t)
	oldConfig := config.Instance
	config.Instance = nil
	t.Cleanup(func() { config.Instance = oldConfig })

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Language", "zh-CN")
	req.Header.Set(HeaderAppLanguage, "my-MM")
	if got := RequestLocale(req); got != "my-MM" {
		t.Fatalf("RequestLocale() = %q, want my-MM", got)
	}
}

func TestRequestLocaleParsesAcceptLanguage(t *testing.T) {
	loadTestLocales(t)
	oldConfig := config.Instance
	config.Instance = nil
	t.Cleanup(func() { config.Instance = oldConfig })

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Accept-Language", "fr-FR, my-MM;q=0.9, en-US;q=0.8")
	if got := RequestLocale(req); got != "my-MM" {
		t.Fatalf("RequestLocale() = %q, want my-MM", got)
	}
}

func TestTranslateMessageToBurmese(t *testing.T) {
	loadTestLocales(t)
	oldConfig := config.Instance
	config.Instance = nil
	t.Cleanup(func() { config.Instance = oldConfig })

	if got := TranslateMessage("帖子不存在", "my-MM"); got != "ပို့စ် မတွေ့ပါ" {
		t.Fatalf("exact translation = %q", got)
	}
	if got := TranslateMessage("悬赏积分需在 10 ~ 50 之间", "my-MM"); got != "ဆုအမှတ်သည် 10 မှ 50 အတွင်း ဖြစ်ရပါမည်" {
		t.Fatalf("formatted translation = %q", got)
	}
	userText := "这是用户自己写的内容"
	if got := TranslateMessage(userText, "my-MM"); got != userText {
		t.Fatalf("user text changed: %q", got)
	}
}

func TestAllLocalesContainSameKeys(t *testing.T) {
	loadTestLocales(t)
	mu.RLock()
	defer mu.RUnlock()

	base := viperInstances["en-US"]
	if base == nil {
		t.Fatal("en-US locale not loaded")
	}
	for locale, v := range viperInstances {
		for _, key := range base.AllKeys() {
			if !v.IsSet(key) {
				t.Errorf("locale %s missing key %s", locale, key)
			}
		}
		for _, key := range v.AllKeys() {
			if !base.IsSet(key) {
				t.Errorf("locale %s has unexpected key %s", locale, key)
			}
		}
	}
}
