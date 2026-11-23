package proxy

import "net/url"

// Line renders the configuration back into the single-line form the GUI
// accepts, so a stored setting can be shown and edited again.
//
// Credentials are escaped the way url.URL does it, which is exactly what Parse
// decodes, so Line and Parse are inverses.
func (c Config) Line() string {
	if c.Kind == "" {
		return c.Addr
	}
	u := url.URL{Scheme: string(c.Kind), Host: c.Addr}
	if c.User != "" || c.Pass != "" {
		u.User = url.UserPassword(c.User, c.Pass)
	}
	return u.String()
}
