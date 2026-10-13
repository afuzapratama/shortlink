package runtime

import (
	"bytes"
	"context"
	"encoding/json"
	"html/template"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type manageLink struct {
	ID, Slug, Destination, State string
	Clicks                       int64
	Routing                      *struct{ Fallback string }
}
type manageView struct {
	Domain struct {
		Hostname, OpeningURL string
		DesiredRevision      int64
	}
	Links            []manageLink
	NextCursor, CSRF string
}
type managePage struct {
	View          *manageView
	Message, Base string
}

var manageHTML = template.Must(template.New("manage").Parse(`<!doctype html><html lang="id"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Manage agent</title><style>body{font:16px system-ui;margin:24px auto;max-width:900px;padding:0 16px}input,button{font:inherit;padding:10px;max-width:100%;box-sizing:border-box}input[type=url]{width:100%}td,th{text-align:left;padding:10px;border-bottom:1px solid #ccc;overflow-wrap:anywhere}.table{overflow:auto}label{display:block;margin:12px 0}p{overflow-wrap:anywhere}</style><h1>Manage agent</h1>{{if .Message}}<p role="alert">{{.Message}}</p>{{end}}{{if .View}}<p>Domain: {{.View.Domain.Hostname}} · revision {{.View.Domain.DesiredRevision}}</p><form method="post" action="/__agent/manage"><input type="hidden" name="action" value="update"><input type="hidden" name="csrf" value="{{.View.CSRF}}"><input type="hidden" name="expectedRevision" value="{{.View.Domain.DesiredRevision}}"><label>URL pembuka domain<input type="url" name="openingUrl" value="{{.View.Domain.OpeningURL}}" maxlength="4096"></label><p>Root dan slug tidak ditemukan diarahkan ke sini. Kosong berarti 503. Perubahan Go menunggu sinkronisasi.</p><button>Simpan URL pembuka</button></form><h2>Shortlink domain ini</h2><div class="table"><table><tr><th>Slug</th><th>Tujuan / fallback</th><th>Status</th><th>Redirect</th></tr>{{range .View.Links}}<tr><td>{{if eq .State "published"}}<a href="{{$.Base}}/{{.Slug}}" target="_blank" rel="noopener noreferrer">{{.Slug}}</a>{{else}}{{.Slug}}{{end}}</td><td>{{.Destination}}{{if .Routing}}<p>Fallback: {{.Routing.Fallback}}</p>{{end}}</td><td>{{.State}}</td><td>{{.Clicks}}</td></tr>{{end}}</table></div>{{if .View.NextCursor}}<a href="/__agent/manage?after={{.View.NextCursor}}">Halaman berikutnya</a>{{end}}<p><a href="/__agent/manage">Muat ulang</a></p><form method="post" action="/__agent/manage"><input type="hidden" name="action" value="logout"><input type="hidden" name="csrf" value="{{.View.CSRF}}"><button>Keluar</button></form>{{else}}<p>Buat key masuk sekali pakai dari detail domain di dashboard pusat. Key berlaku lima menit.</p><form method="post" action="/__agent/manage"><input type="hidden" name="action" value="login"><label>Key akses manage<input type="password" name="key" required maxlength="43" autocomplete="off"></label><button>Masuk</button></form>{{end}}</html>`))

func (a *Agent) manageCall(ctx context.Context, path string, body any) (json.RawMessage, int) {
	raw, _ := json.Marshal(body)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	response, err := a.request(ctx, http.MethodPost, path, bytes.NewReader(raw))
	if err != nil {
		return nil, 503
	}
	defer response.Body.Close()
	raw, err = io.ReadAll(io.LimitReader(response.Body, 128*1024+1))
	if err != nil || len(raw) > 128*1024 {
		return nil, 503
	}
	return raw, response.StatusCode
}
func (a *Agent) manageCookie(w http.ResponseWriter, value string, expires time.Time) {
	age := int(time.Until(expires).Seconds())
	if value == "" {
		age = -1
	}
	http.SetCookie(w, &http.Cookie{Name: "sl_agent_manage", Value: value, Path: "/__agent/manage", HttpOnly: true, Secure: !a.cfg.Development, SameSite: http.SameSiteStrictMode, MaxAge: age, Expires: expires})
}
func (a *Agent) manage(w http.ResponseWriter, r *http.Request) {
	host := r.Host
	if strings.Contains(host, ":") {
		var err error
		host, _, err = net.SplitHostPort(host)
		if err != nil {
			http.Error(w, "invalid host", 400)
			return
		}
	}
	if normalizeHost(host) != a.cfg.Hostname {
		http.Error(w, "unknown host", 421)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; form-action 'self'; frame-ancestors 'none'; base-uri 'none'")
	w.Header().Set("Referrer-Policy", "no-referrer")
	if r.Method != "GET" && r.Method != "POST" {
		w.Header().Set("Allow", "GET, POST")
		http.Error(w, "method not allowed", 405)
		return
	}
	session := ""
	if cookie, err := r.Cookie("sl_agent_manage"); err == nil {
		session = cookie.Value
	}
	var view manageView
	page := managePage{Base: "https://" + a.cfg.Hostname}
	if a.cfg.Development {
		page.Base = "http://" + r.Host
	}
	if r.Method == "POST" {
		origin, err := url.Parse(r.Header.Get("Origin"))
		scheme := "https"
		if a.cfg.Development {
			scheme = "http"
		}
		if err != nil || origin.Scheme != scheme || origin.Host != r.Host || origin.User != nil || origin.Path != "" || origin.RawQuery != "" || origin.Fragment != "" {
			http.Error(w, "origin ditolak", 403)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		if r.ParseForm() != nil {
			http.Error(w, "form tidak valid", 400)
			return
		}
		action := r.PostForm.Get("action")
		if action == "login" {
			raw, status := a.manageCall(r.Context(), "/v1/agents/manage/exchange", map[string]string{"key": r.PostForm.Get("key")})
			var grant struct {
				Session   string
				ExpiresAt time.Time
			}
			if status == 200 && json.Unmarshal(raw, &grant) == nil && len(grant.Session) == 43 && grant.ExpiresAt.After(time.Now()) {
				a.manageCookie(w, grant.Session, grant.ExpiresAt)
				http.Redirect(w, r, "/__agent/manage", 303)
				return
			}
			if status >= 500 {
				w.WriteHeader(503)
			} else {
				w.WriteHeader(401)
			}
			page.Message = "Key tidak valid, kedaluwarsa, atau pusat belum tersedia."
			_ = manageHTML.Execute(w, page)
			return
		}
		body := map[string]any{"session": session, "csrf": r.PostForm.Get("csrf"), "action": action}
		if action == "update" {
			var revision int64
			if json.Unmarshal([]byte(r.PostForm.Get("expectedRevision")), &revision) != nil {
				http.Error(w, "revision tidak valid", 400)
				return
			}
			body["expectedRevision"] = revision
			body["openingUrl"] = r.PostForm.Get("openingUrl")
		}
		raw, status := a.manageCall(r.Context(), "/v1/agents/manage/session", body)
		if status == 200 {
			if action == "logout" {
				a.manageCookie(w, "", time.Unix(1, 0))
			}
			http.Redirect(w, r, "/__agent/manage", 303)
			return
		}
		if status == 403 {
			http.Error(w, "akses ditolak", 403)
			return
		}
		page.Message = "Perubahan belum tersimpan. Muat ulang jika revision berubah; periksa URL dan koneksi pusat."
		if status >= 500 {
			w.WriteHeader(503)
		}
		_ = raw
	}
	if session != "" {
		raw, status := a.manageCall(r.Context(), "/v1/agents/manage/session", map[string]string{"action": "view", "session": session, "after": r.URL.Query().Get("after")})
		if status == 200 && json.Unmarshal(raw, &view) == nil && view.Domain.Hostname == a.cfg.Hostname {
			page.View = &view
		} else if status == 401 {
			a.manageCookie(w, "", time.Unix(1, 0))
			page.Message = "Sesi berakhir. Buat key baru dari pusat."
		} else {
			page.Message = "Pusat belum tersedia atau data tidak valid."
			if r.Method == "GET" {
				w.WriteHeader(503)
			}
		}
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_ = manageHTML.Execute(w, page)
}
