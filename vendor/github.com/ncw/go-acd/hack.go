package acd

import (
	"encoding/json"
	"log"
	"net/http"
	"net/url"
	"os"

	"github.com/rmdashrf/go-misc/cookiejar2"
	"github.com/rmdashrf/go-misc/editthiscookie"
)

var (
	CookieHack bool
	Jar        http.CookieJar
	Cookies    []*http.Cookie
	SessionId  string
)

func init() {
	cookieHackFile := os.Getenv("ACD_COOKIEHACK")
	if cookieHackFile != "" {
		loadCookies(cookieHackFile)
	}
}

func loadCookies(file string) {
	f, err := os.Open(file)
	if err != nil {
		log.Printf("Cookiehack: failed to open cookiefile: %v\n", err)
		return
	}

	defer f.Close()

	var entries []editthiscookie.Entry
	if err := json.NewDecoder(f).Decode(&entries); err != nil {
		log.Printf("Could not load cookies: %v\n", err)
		return
	}

	for _, e := range entries {
		Cookies = append(Cookies, e.GoCookie())
		if e.Name == "session-id" {
			SessionId = e.Value
		}
	}

	log.Printf("cookiehack: loaded %d cookies with sessionid %s\n", len(Cookies), SessionId)
	CookieHack = true

	jar := cookiejar2.New(nil)
	aznUrl, _ := url.Parse("https://www.amazon.com")
	jar.SetCookies(aznUrl, Cookies)
	Jar = jar
}
