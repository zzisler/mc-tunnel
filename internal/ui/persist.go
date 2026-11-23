//go:build windows

package ui

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"mctunnel/internal/proxy"
	"mctunnel/internal/secret"
	"mctunnel/internal/settings"
)

// dpapiProtector stores the proxy password with the Windows Data Protection
// API, so the file on disk is useless on another machine or for another user.
type dpapiProtector struct{}

// Protect implements settings.Protector.
func (dpapiProtector) Protect(plaintext string) (string, error) { return secret.Protect(plaintext) }

// Unprotect implements settings.Protector.
func (dpapiProtector) Unprotect(blob string) (string, error) { return secret.Unprotect(blob) }

// settingsFromForm converts the form into the persistable configuration, using
// the same parsing rules the Start button uses.
func settingsFromForm(in formValues, logsVisible bool) (settings.Settings, error) {
	cfg, _, _, err := proxyConfig(in)
	if err != nil {
		return settings.Settings{}, err
	}
	port, err := parseLocalPort(in.Port)
	if err != nil {
		return settings.Settings{}, err
	}
	return settings.Settings{
		Server:        strings.TrimSpace(in.Server),
		ProxyKind:     string(cfg.Kind),
		ProxyAddr:     cfg.Addr,
		ProxyUser:     cfg.User,
		ProxyPassword: cfg.Pass,
		LocalPort:     port,
		LogsVisible:   logsVisible,
	}, nil
}

// proxyLineFromSettings rebuilds the single proxy line the user edits.
func proxyLineFromSettings(s settings.Settings) string {
	if s.ProxyAddr == "" {
		return ""
	}
	cfg := proxy.Config{
		Kind: proxy.Kind(s.ProxyKind),
		Addr: s.ProxyAddr,
		User: s.ProxyUser,
		Pass: s.ProxyPassword,
	}
	return cfg.Line()
}

// applySettings fills the form from stored settings.
func (a *App) applySettings(s settings.Settings) {
	setText(a.edServer, s.Server)
	setText(a.edProxy, proxyLineFromSettings(s))
	if s.ProxyKind != "" {
		a.setKind(s.ProxyKind)
	}
	setText(a.edPort, strconv.Itoa(s.LocalPort))
	a.setLogsVisible(s.LogsVisible)
}

// loadSettings reads the stored configuration, keeping an unreadable file as a
// .bak instead of silently overwriting it later.
func (a *App) loadSettings() {
	path, pathErr := settings.ResolvePath()
	if path == "" {
		a.log.Warnf("settings are disabled: %v", pathErr)
		return
	}
	if pathErr != nil {
		// The path is still usable information: say where saving will fail.
		a.log.Warnf("settings: %v", pathErr)
	}
	a.settingsPath = path

	// When the file is not where the user would expect it, the reason travels
	// with every "saved to" line: a config.json next to the exe must not be a
	// mystery a scroll to the top of the log would explain.
	a.settingsNote = ""
	if preferred, err := os.UserConfigDir(); err == nil {
		usual := filepath.Join(preferred, "mctunnel", "config.json")
		if filepath.Clean(path) != filepath.Clean(usual) {
			if os.Getenv(settings.ConfigPathEnv) != "" {
				a.settingsNote = " (path from " + settings.ConfigPathEnv + ")"
			} else {
				a.settingsNote = " (the roaming profile is not writable)"
			}
			a.log.Infof("settings live in %s%s", path, a.settingsNote)
		}
	}

	stored, err := settings.Load(path, a.protector)
	if err != nil {
		if backup, backupErr := settings.Backup(path); backupErr == nil {
			a.log.Warnf("settings: %v", err)
			a.log.Warnf("the old file was kept as %s, starting with defaults", backup)
		} else {
			a.log.Warnf("settings: %v, starting with defaults", err)
		}
		stored = settings.Default()
	}
	a.initial = stored
	a.log.Infof("settings loaded from %s", path)
}

// saveSettings writes the current form to disk. Failures are logged, never
// fatal: losing persistence must not break a working tunnel.
func (a *App) saveSettings(quiet bool) {
	if a.settingsPath == "" {
		return
	}
	stored, err := settingsFromForm(a.readForm(), a.logsVisible)
	if err != nil {
		if !quiet {
			a.log.Warnf("settings were not saved: %v", err)
		}
		return
	}
	if err := settings.Save(a.settingsPath, stored, a.protector); err != nil {
		a.log.Warnf("settings were not saved: %v", err)
		return
	}
	if !quiet {
		a.log.Infof("settings saved to %s%s", a.settingsPath, a.settingsNote)
	}
}
