package editthiscookie

import (
	"net/http"
	"strings"
	"time"
)

type Entry struct {
	Domain         string `json:"domain"`
	ExpirationDate float64
	HostOnly       bool   `json:"hostOnly"`
	HttpOnly       bool   `json:"httpOnly"`
	Name           string `json:"name"`
	Path           string `json:"path"`
	SameSite       string `json:"sameSite"`
	Secure         bool   `json:"secure"`
	Session        bool   `json:"session"`
	StoreId        string `json"storeId"`
	Value          string `json:"value"`
	Id             int    `json:"id"`
}

func (e *Entry) GoCookie() *http.Cookie {
	expiration := time.Unix(int64(e.ExpirationDate), 0)

	return &http.Cookie{
		Name:     e.Name,
		Value:    strings.Replace(e.Value, "\"", "", -1),
		Path:     e.Path,
		Domain:   e.Domain,
		Expires:  expiration,
		Secure:   e.Secure,
		HttpOnly: e.HttpOnly,
	}
}
