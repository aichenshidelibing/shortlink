package api

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"html"
	"net/http"
	"net/url"
	"shortlink/internal/crypto"
	"shortlink/internal/model"
	"shortlink/internal/repository"
	"shortlink/internal/service"
	"shortlink/internal/worker"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

type RedirectHandler struct {
	linkSvc     *service.LinkService
	linkRepo    *repository.LinkRepository
	clickRepo   *repository.ClickRepository
	clickWorker *worker.ClickWorker
	strong      *crypto.StrongCrypto
	statsSvc    *service.StatsService
}

func NewRedirectHandler(linkSvc *service.LinkService, linkRepo *repository.LinkRepository, clickRepo *repository.ClickRepository, clickWorker *worker.ClickWorker, strong *crypto.StrongCrypto, statsSvc *service.StatsService) *RedirectHandler {
	return &RedirectHandler{linkSvc: linkSvc, linkRepo: linkRepo, clickRepo: clickRepo, clickWorker: clickWorker, strong: strong, statsSvc: statsSvc}
}

func (h *RedirectHandler) Redirect(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.Data(http.StatusNotFound, "text/html; charset=utf-8", errorPage(http.StatusNotFound, "短链不存在", "这个短链可能已经过期、被删除，或输入有误。", "请检查地址是否完整，也可以返回首页重新生成一个新的短链。"))
		return
	}

	url, link, err := h.linkSvc.Resolve(c.Request.Context(), code)
	if err != nil {
		c.Data(http.StatusNotFound, "text/html; charset=utf-8", errorPage(http.StatusNotFound, "短链不存在", "这个短链可能已经过期、被删除，或输入有误。", "请检查地址是否完整，也可以返回首页重新生成一个新的短链。"))
		return
	}

	if link != nil && link.PasswordHash != "" {
		if !h.hasPasswordAccess(c, code, link) {
			c.Data(http.StatusOK, "text/html; charset=utf-8", passwordPage(code, false))
			return
		}
	}

	if link != nil && link.RequiresConfirm && c.Query("confirm") != "1" {
		c.Data(http.StatusOK, "text/html; charset=utf-8", confirmPage(code, url, link.RiskReasons))
		return
	}

	// One-shot links are consumed AFTER any password gate has been passed —
	// otherwise merely rendering the password entry page (which also hits
	// this handler) would burn the link, so the user's first correct
	// password attempt would then find nothing to redirect to.
	if link != nil && link.IsOnce {
		marked, err := h.linkSvc.ConsumeOnce(c.Request.Context(), code)
		if err != nil || !marked {
			c.Data(http.StatusNotFound, "text/html; charset=utf-8", errorPage(http.StatusNotFound, "短链不存在", "这个短链可能已经过期、被删除，或输入有误。", "请检查地址是否完整，也可以返回首页重新生成一个新的短链。"))
			return
		}
	}

	go h.recordClickAsync(link, c.ClientIP(), c.GetHeader("Referer"), c.GetHeader("User-Agent"))

	c.Redirect(http.StatusFound, url)
}

func (h *RedirectHandler) SubmitPassword(c *gin.Context) {
	code := c.Param("code")
	if code == "" {
		c.Data(http.StatusNotFound, "text/html; charset=utf-8", errorPage(http.StatusNotFound, "短链不存在", "这个短链可能已经过期、被删除，或输入有误。", "请检查地址是否完整，也可以返回首页重新生成一个新的短链。"))
		return
	}
	_, link, err := h.linkSvc.Resolve(c.Request.Context(), code)
	if err != nil || link == nil || link.PasswordHash == "" {
		c.Data(http.StatusNotFound, "text/html; charset=utf-8", errorPage(http.StatusNotFound, "一次性链接已失效", "这个链接可能已经被访问过。", "一次性短链只允许成功访问一次，请联系链接创建者重新生成。"))
		return
	}
	password := c.PostForm("password")
	if !h.linkSvc.VerifyPassword(link, password) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", passwordPage(code, true))
		return
	}
	h.setPasswordAccess(c, code, link)
	c.Redirect(http.StatusSeeOther, "/"+url.PathEscape(code))
}

func passwordCookieName(code string) string {
	return "sl_pw_" + hashShort(code)[:16]
}

func passwordCookieValue(code string, link *model.Link) string {
	return hashShort(code + ":" + link.PasswordHash)
}

func (h *RedirectHandler) hasPasswordAccess(c *gin.Context, code string, link *model.Link) bool {
	cookie, err := c.Cookie(passwordCookieName(code))
	return err == nil && cookie == passwordCookieValue(code, link)
}

func (h *RedirectHandler) setPasswordAccess(c *gin.Context, code string, link *model.Link) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     passwordCookieName(code),
		Value:    passwordCookieValue(code, link),
		Path:     "/" + code,
		MaxAge:   10 * 60,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https"),
	})
}

func errorPage(status int, title, message, detail string) []byte {
	statusText := http.StatusText(status)
	if statusText == "" {
		statusText = "Error"
	}
	return []byte(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1.0"><meta name="robots" content="noindex"><title>` + html.EscapeString(title) + ` - Shortlink</title><style>
		:root{color-scheme:light dark;--bg:#f7f8ff;--card:rgba(255,255,255,.78);--text:#111827;--muted:#667085;--line:rgba(99,102,241,.18);--accent:#4f46e5;--accent2:#06b6d4;--danger:#ef4444;--shadow:0 24px 80px rgba(79,70,229,.18)}@media(prefers-color-scheme:dark){:root{--bg:#080b16;--card:rgba(15,23,42,.78);--text:#f8fafc;--muted:#94a3b8;--line:rgba(148,163,184,.18);--shadow:0 28px 90px rgba(0,0,0,.42)}}*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;padding:24px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','Noto Sans SC',Roboto,sans-serif;color:var(--text);background:radial-gradient(circle at 20% 10%,rgba(79,70,229,.22),transparent 34%),radial-gradient(circle at 78% 18%,rgba(6,182,212,.18),transparent 30%),linear-gradient(135deg,var(--bg),var(--bg))}.card{position:relative;width:min(560px,100%);overflow:hidden;border:1px solid var(--line);border-radius:30px;background:var(--card);box-shadow:var(--shadow);backdrop-filter:blur(22px);padding:34px}.card:before{content:"";position:absolute;inset:0 0 auto;height:5px;background:linear-gradient(90deg,var(--accent),var(--accent2),#a855f7)}.code{display:inline-flex;align-items:center;gap:8px;padding:8px 12px;border-radius:999px;background:rgba(239,68,68,.1);color:var(--danger);font-weight:900;font-size:13px}.icon{width:72px;height:72px;margin:22px 0 16px;border-radius:24px;display:grid;place-items:center;background:linear-gradient(135deg,rgba(79,70,229,.16),rgba(6,182,212,.16));font-size:34px}h1{margin:0 0 10px;font-size:clamp(28px,5vw,42px);letter-spacing:-.04em}p{margin:0;color:var(--muted);line-height:1.8}.detail{margin-top:16px;padding:15px 16px;border-radius:18px;background:rgba(148,163,184,.1);border:1px solid var(--line);font-size:14px}.actions{display:flex;gap:12px;flex-wrap:wrap;margin-top:26px}.btn{flex:1;min-width:150px;text-align:center;text-decoration:none;border-radius:16px;padding:13px 16px;font-weight:900}.primary{color:white;background:linear-gradient(135deg,var(--accent),var(--accent2));box-shadow:0 14px 30px rgba(79,70,229,.25)}.ghost{color:var(--text);background:rgba(148,163,184,.12);border:1px solid var(--line)}.foot{margin-top:22px;font-size:12px;color:var(--muted)}
		</style></head><body><main class="card"><span class="code">` + html.EscapeString(statusText) + ` · ` + html.EscapeString(http.StatusText(status)) + `</span><div class="icon">🧭</div><h1>` + html.EscapeString(title) + `</h1><p>` + html.EscapeString(message) + `</p><div class="detail">` + html.EscapeString(detail) + `</div><div class="actions"><a class="btn primary" href="/">返回首页</a><a class="btn ghost" href="/help">查看帮助</a></div><div class="foot">Shortlink 已拦截这个无效访问，没有泄露目标地址。</div></main></body></html>`)
}

func confirmPage(code, rawURL, reasons string) []byte {
	u, _ := url.Parse(rawURL)
	host := html.EscapeString(u.Hostname())
	cont := "/" + url.PathEscape(code) + "?confirm=1"
	reasonText := strings.TrimSpace(reasons)
	if reasonText == "" {
		reasonText = "目标链接触发了安全确认策略"
	}
	return []byte(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1.0"><meta name="robots" content="noindex"><title>即将离开本站 - Shortlink</title><style>
		:root{color-scheme:light dark;--bg:#f7f8ff;--card:rgba(255,255,255,.8);--text:#111827;--muted:#667085;--line:rgba(245,158,11,.24);--warn:#f59e0b;--accent:#4f46e5;--shadow:0 24px 80px rgba(245,158,11,.16)}@media(prefers-color-scheme:dark){:root{--bg:#080b16;--card:rgba(15,23,42,.8);--text:#f8fafc;--muted:#94a3b8;--line:rgba(245,158,11,.28);--shadow:0 28px 90px rgba(0,0,0,.42)}}*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;padding:24px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','Noto Sans SC',Roboto,sans-serif;color:var(--text);background:radial-gradient(circle at 20% 10%,rgba(245,158,11,.22),transparent 34%),radial-gradient(circle at 78% 18%,rgba(79,70,229,.18),transparent 30%),linear-gradient(135deg,var(--bg),var(--bg))}.card{width:min(620px,100%);border:1px solid var(--line);border-radius:30px;background:var(--card);box-shadow:var(--shadow);backdrop-filter:blur(22px);padding:34px}.badge{display:inline-flex;padding:8px 12px;border-radius:999px;background:rgba(245,158,11,.12);color:var(--warn);font-weight:900;font-size:13px}.icon{width:72px;height:72px;margin:22px 0 16px;border-radius:24px;display:grid;place-items:center;background:rgba(245,158,11,.14);font-size:34px}h1{margin:0 0 10px;font-size:clamp(28px,5vw,42px);letter-spacing:-.04em}p{margin:0;color:var(--muted);line-height:1.8}.host{margin:18px 0;padding:16px 18px;border-radius:18px;background:rgba(148,163,184,.1);border:1px solid var(--line);font-weight:900;word-break:break-all}.risk{font-size:14px;color:var(--muted);line-height:1.8}.actions{display:flex;gap:12px;flex-wrap:wrap;margin-top:26px}.btn{flex:1;min-width:150px;text-align:center;text-decoration:none;border-radius:16px;padding:13px 16px;font-weight:900}.go{color:white;background:linear-gradient(135deg,#f59e0b,#ef4444)}.back{color:var(--text);background:rgba(148,163,184,.12);border:1px solid var(--line)}
		</style></head><body><main class="card"><span class="badge">安全确认</span><div class="icon">⚠️</div><h1>即将访问外部链接</h1><p>这个短链指向站外地址，请确认目标域名可信后再继续。</p><div class="host">` + host + `</div><div class="risk">原因：` + html.EscapeString(reasonText) + `</div><div class="actions"><a class="btn back" href="/">返回首页</a><a class="btn go" href="` + html.EscapeString(cont) + `">确认并继续</a></div></main></body></html>`)
}

func passwordPage(code string, hasError bool) []byte {
	title := "此链接已加密"
	message := "请输入访问密码后继续跳转。"
	icon := "🔒"
	if hasError {
		title = "密码不正确"
		message = "请检查密码后再试一次。"
		icon = "🔐"
	}
	return []byte(`<!DOCTYPE html><html lang="zh-CN"><head><meta charset="UTF-8"><meta name="viewport" content="width=device-width,initial-scale=1.0"><meta name="robots" content="noindex"><title>需要密码 - Shortlink</title><style>
		:root{color-scheme:light dark;--bg:#f7f8ff;--card:rgba(255,255,255,.82);--text:#111827;--muted:#667085;--line:rgba(99,102,241,.18);--accent:#4f46e5;--accent2:#06b6d4;--danger:#ef4444;--shadow:0 24px 80px rgba(79,70,229,.18)}@media(prefers-color-scheme:dark){:root{--bg:#080b16;--card:rgba(15,23,42,.82);--text:#f8fafc;--muted:#94a3b8;--line:rgba(148,163,184,.18);--shadow:0 28px 90px rgba(0,0,0,.42)}}*{box-sizing:border-box}body{margin:0;min-height:100vh;display:grid;place-items:center;padding:24px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI','Noto Sans SC',Roboto,sans-serif;color:var(--text);background:radial-gradient(circle at 20% 10%,rgba(79,70,229,.22),transparent 34%),radial-gradient(circle at 78% 18%,rgba(6,182,212,.18),transparent 30%),linear-gradient(135deg,var(--bg),var(--bg))}.card{width:min(440px,100%);border:1px solid var(--line);border-radius:30px;background:var(--card);box-shadow:var(--shadow);backdrop-filter:blur(22px);padding:34px;text-align:center}.icon{width:76px;height:76px;margin:0 auto 18px;border-radius:26px;display:grid;place-items:center;background:linear-gradient(135deg,rgba(79,70,229,.16),rgba(6,182,212,.16));font-size:34px}h1{margin:0 0 10px;font-size:30px;letter-spacing:-.03em}p{margin:0 0 22px;color:var(--muted);line-height:1.7}.err{color:var(--danger)}input{width:100%;padding:14px 15px;border-radius:16px;border:1px solid var(--line);background:rgba(148,163,184,.1);color:var(--text);font-size:15px;outline:none;margin-bottom:12px}input:focus{border-color:var(--accent);box-shadow:0 0 0 4px rgba(79,70,229,.12)}button{width:100%;padding:14px;border-radius:16px;border:none;background:linear-gradient(135deg,var(--accent),var(--accent2));color:#fff;font-size:15px;font-weight:900;cursor:pointer;box-shadow:0 14px 30px rgba(79,70,229,.25)}.back{display:inline-block;margin-top:18px;color:var(--muted);text-decoration:none;font-size:13px}
		</style></head><body><main class="card"><div class="icon">` + icon + `</div><h1>` + html.EscapeString(title) + `</h1><p class="` + map[bool]string{true: "err", false: ""}[hasError] + `">` + html.EscapeString(message) + `</p><form method="post" action="/` + html.EscapeString(url.PathEscape(code)) + `"><input type="password" name="password" placeholder="输入访问密码" autocomplete="current-password" autofocus required><button type="submit">解锁访问</button></form><a class="back" href="/">返回首页</a></main></body></html>`)
}

func hashShort(v string) string {
	if v == "" {
		return ""
	}
	h := sha256.Sum256([]byte(v))
	return hex.EncodeToString(h[:])
}

func parseUA(ua string) (device, browser, osName string, isBot bool) {
	l := strings.ToLower(ua)
	device = "desktop"
	browser = "other"
	osName = "other"
	if strings.Contains(l, "bot") || strings.Contains(l, "spider") || strings.Contains(l, "crawl") {
		isBot = true
		device = "bot"
	}
	if strings.Contains(l, "mobile") || strings.Contains(l, "iphone") || strings.Contains(l, "android") {
		device = "mobile"
	}
	if strings.Contains(l, "ipad") || strings.Contains(l, "tablet") {
		device = "tablet"
	}
	if strings.Contains(l, "chrome") {
		browser = "chrome"
	} else if strings.Contains(l, "firefox") {
		browser = "firefox"
	} else if strings.Contains(l, "safari") {
		browser = "safari"
	} else if strings.Contains(l, "edge") {
		browser = "edge"
	}
	if strings.Contains(l, "windows") {
		osName = "windows"
	} else if strings.Contains(l, "mac os") || strings.Contains(l, "macintosh") {
		osName = "macos"
	} else if strings.Contains(l, "android") {
		osName = "android"
	} else if strings.Contains(l, "iphone") || strings.Contains(l, "ios") {
		osName = "ios"
	} else if strings.Contains(l, "linux") {
		osName = "linux"
	}
	return
}

func (h *RedirectHandler) recordClickAsync(link *model.Link, ip, referer, userAgent string) {
	ctx := context.Background()
	if link == nil {
		return
	}
	ipEnc, nonce, _ := h.strong.Encrypt([]byte(ip))

	geo, _ := h.statsSvc.RecordClick(ctx, link.ID, ip)
	country := ""
	region := ""
	if geo != nil {
		country = geo.Country
		region = geo.Region
	}

	now := time.Now()
	refHost := ""
	if u, err := url.Parse(referer); err == nil {
		refHost = u.Hostname()
	}
	device, browser, osName, isBot := parseUA(userAgent)
	click := &model.Click{
		LinkID: link.ID, Country: country, Region: region, IPEnc: ipEnc, IPNonce: nonce,
		RefererHost: refHost, UserAgentHash: hashShort(userAgent), VisitorHash: hashShort(ip + now.Format("2006-01-02") + link.ShortCode),
		Browser: browser, OS: osName, Device: device, IsBot: isBot, Hour: now.Hour(), EventType: "click", ClickedAt: now,
	}
	h.clickWorker.Submit(click)
	if err := h.linkRepo.IncrementClick(ctx, link.ShortCode); err != nil {
		// click count is best-effort; log and continue
		_ = err
	}
}
