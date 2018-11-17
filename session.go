package egret

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// Session A signed cookie (and thus limited to 4kb in size).
// Restriction: Keys may not have a colon in them.
type Session map[string]string

const (
	SessionIDKey = "_ID"
	TimestampKey = "_TS"
)

// expireAfterDuration is the time to live, in seconds, of a session cookie.
// It may be specified in config as "session.expires". Values greater than 0
// set a persistent cookie with a time to live as specified, and the value 0
// sets a session cookie.
var expireAfterDuration time.Duration

func init() {
	// Set expireAfterDuration, default to 30 days if no value in config
	OnAppStart(func() {
		expireAfterDuration = Config.GetDuration("session.expires")
	})
}

// ID retrieves from the cookie or creates a time-based UUID identifying this
// session.
func (s Session) ID() string {
	if sessionIDStr, ok := s[SessionIDKey]; ok {
		return sessionIDStr
	}

	buffer := make([]byte, 32)
	if _, err := rand.Read(buffer); err != nil {
		panic(err)
	}

	s[SessionIDKey] = hex.EncodeToString(buffer)
	return s[SessionIDKey]
}

// getExpiration return a time.Time with the session's expiration date.
// If previous session has set to "session", remain it
func (s Session) getExpiration() time.Time {
	if expireAfterDuration == 0 || s[TimestampKey] == "session" {
		// Expire after closing browser
		return time.Time{}
	}
	return time.Now().Add(expireAfterDuration)
}

// Cookie returns an http.Cookie containing the signed session.
func (s Session) Cookie() *http.Cookie {
	var sessionValue string
	ts := s.getExpiration()
	s[TimestampKey] = getSessionExpirationCookie(ts)
	for key, value := range s {
		if strings.ContainsAny(key, ":\x00") {
			panic("Session keys may not have colons or null bytes")
		}
		if strings.Contains(value, "\x00") {
			panic("Session values may not have null bytes")
		}
		sessionValue += "\x00" + key + ":" + value + "\x00"
	}

	sessionData := url.QueryEscape(sessionValue)
	return &http.Cookie{
		Name:     CookiePrefix + "_SESSION",
		Value:    Sign(sessionData) + "-" + sessionData,
		Domain:   CookieDomain,
		Path:     "/",
		HttpOnly: true,
		Secure:   CookieSecure,
		Expires:  ts.UTC(),
	}
}

// sessionTimeoutExpiredOrMissing returns a boolean of whether the session
// cookie is either not present or present but beyond its time to live; i.e.,
// whether there is not a valid session.
func sessionTimeoutExpiredOrMissing(session Session) bool {
	if exp, present := session[TimestampKey]; !present {
		return true
	} else if exp == "session" {
		return false
	} else if expInt, _ := strconv.Atoi(exp); int64(expInt) < time.Now().Unix() {
		return true
	}
	return false
}

// GetSessionFromCookie returns a Session struct pulled from the signed
// session cookie.
func GetSessionFromCookie(cookie *http.Cookie) Session {
	session := make(Session)

	// Separate the data from the signature.
	hyphen := strings.Index(cookie.Value, "-")
	if hyphen == -1 || hyphen >= len(cookie.Value)-1 {
		return session
	}
	sig, data := cookie.Value[:hyphen], cookie.Value[hyphen+1:]

	// Verify the signature.
	if !Verify(data, sig) {
		Logger.Warn("Session cookie signature failed")
		return session
	}

	ParseKeyValueCookie(data, func(key, val string) {
		session[key] = val
	})

	if sessionTimeoutExpiredOrMissing(session) {
		session = make(Session)
	}

	return session
}

// SessionHandler is a Egret Handler that retrieves and sets the session cookie.
// Within Egret, it is available as a Session attribute on Context instances.
// The name of the Session cookie is set as CookiePrefix + "_SESSION".
func SessionHandler(ctx *Context) {
	ctx.Session = restoreSession(ctx.Request.Request)
	sessionWasEmpty := len(ctx.Session) == 0

	// Make session vars available in templates as {{.session.xyz}}
	ctx.RenderArgs["session"] = ctx.Session

	// Store the signed session if it could have changed.
	if len(ctx.Session) > 0 || !sessionWasEmpty {
		ctx.SetCookie(ctx.Session.Cookie())
	}
	ctx.Next()
}

// restoreSession returns either the current session, retrieved from the
// session cookie, or a new session.
func restoreSession(req *http.Request) Session {
	cookie, err := req.Cookie(CookiePrefix + "_SESSION")
	if err != nil {
		return make(Session)
	}
	return GetSessionFromCookie(cookie)
}

// getSessionExpirationCookie retrieves the cookie's time to live as a
// string of either the number of seconds, for a persistent cookie, or
// "session".
func getSessionExpirationCookie(t time.Time) string {
	if t.IsZero() {
		return "session"
	}
	return strconv.FormatInt(t.Unix(), 10)
}

// SetNoExpiration sets session to expire when browser session ends
func (s Session) SetNoExpiration() {
	s[TimestampKey] = "session"
}

// SetDefaultExpiration sets session to expire after default duration
func (s Session) SetDefaultExpiration() {
	delete(s, TimestampKey)
}
