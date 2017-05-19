package cookiejar2

import (
	"net/http"
	"net/url"
)

type ImmutableCookieJar struct {
	Inner http.CookieJar
}

func (i *ImmutableCookieJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	// Immutable, prevent set cookies
}

func (i *ImmutableCookieJar) Cookies(u *url.URL) []*http.Cookie {
	return i.Inner.Cookies(u)
}
