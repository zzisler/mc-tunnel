// Package settings stores the small amount of state the program remembers
// between runs: the server, the proxy, the local port and whether the log panel
// was open.
//
// Two rules shape it:
//
//   - the file on disk never contains the proxy password in clear text; it is
//     handed to a Protector (Windows DPAPI in the real build) and only the blob
//     is written;
//   - saving is atomic, so a crash or a full disk cannot leave a half written
//     config behind.
package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Version is the schema version written into the file. A file with another
// version is refused instead of being guessed at.
const Version = 1

// utf8BOM is stripped from a loaded file: Notepad writes one and JSON does not
// accept it.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// DefaultPort is the local port used when nothing is stored.
const DefaultPort = 25565

// Settings is the in-memory configuration. ProxyPassword is plaintext here and
// exists only inside the process; it is encrypted by Save.
type Settings struct {
	Server        string
	ProxyKind     string
	ProxyAddr     string
	ProxyUser     string
	ProxyPassword string
	LocalPort     int
	LogsVisible   bool
}

// Protector encrypts a secret for storage and decrypts it back.
type Protector interface {
	Protect(plaintext string) (string, error)
	Unprotect(blob string) (string, error)
}

// Default returns the settings used when nothing has been saved yet.
func Default() Settings {
	return Settings{LocalPort: DefaultPort}
}

// ConfigPathEnv overrides the configuration location. It makes a portable
// install possible (keep the file next to the exe) and lets tests point at a
// temporary directory.
const ConfigPathEnv = "MCTUNNEL_CONFIG"

// DefaultPath is %APPDATA%\mctunnel\config.json on Windows, unless
// MCTUNNEL_CONFIG points somewhere else.
func DefaultPath() (string, error) {
	if custom := strings.TrimSpace(os.Getenv(ConfigPathEnv)); custom != "" {
		return custom, nil
	}
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("cannot find the user configuration directory: %w", err)
	}
	return filepath.Join(dir, "mctunnel", "config.json"), nil
}

// file is the on-disk representation. The password only ever appears as
// ProxySecret, an encrypted blob.
type file struct {
	Version     int    `json:"version"`
	Server      string `json:"server,omitempty"`
	ProxyKind   string `json:"proxy_kind,omitempty"`
	ProxyAddr   string `json:"proxy_addr,omitempty"`
	ProxyUser   string `json:"proxy_user,omitempty"`
	ProxySecret string `json:"proxy_secret,omitempty"`
	LocalPort   int    `json:"local_port"`
	LogsVisible bool   `json:"logs_visible"`
}

// Load reads the configuration. A missing file is not an error: it means the
// program has never run before, so Default() is returned.
func Load(path string, protector Protector) (Settings, error) {
	out := Default()

	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	// Notepad happily writes a UTF-8 BOM, and a config edited by hand must still
	// load. The marker is dropped before parsing.
	data = bytes.TrimPrefix(data, utf8BOM)

	var stored file
	if err := json.Unmarshal(data, &stored); err != nil {
		return out, fmt.Errorf("%s is not valid JSON: %w", path, err)
	}
	if stored.Version != Version {
		return out, fmt.Errorf("%s has version %d, this build understands %d", path, stored.Version, Version)
	}

	out.Server = stored.Server
	out.ProxyKind = stored.ProxyKind
	out.ProxyAddr = stored.ProxyAddr
	out.ProxyUser = stored.ProxyUser
	if stored.LocalPort > 0 && stored.LocalPort <= 65535 {
		out.LocalPort = stored.LocalPort
	}
	out.LogsVisible = stored.LogsVisible

	if stored.ProxySecret != "" {
		if protector == nil {
			return out, errors.New("the password is stored encrypted but no decryptor is available")
		}
		password, err := protector.Unprotect(stored.ProxySecret)
		if err != nil {
			return out, fmt.Errorf("proxy password: %w", err)
		}
		out.ProxyPassword = password
	}
	return out, nil
}

// Save writes the configuration atomically: a temporary file in the same
// directory is synced and then renamed over the target.
func Save(path string, s Settings, protector Protector) error {
	stored := file{
		Version:     Version,
		Server:      s.Server,
		ProxyKind:   s.ProxyKind,
		ProxyAddr:   s.ProxyAddr,
		ProxyUser:   s.ProxyUser,
		LocalPort:   s.LocalPort,
		LogsVisible: s.LogsVisible,
	}
	if s.ProxyPassword != "" {
		if protector == nil {
			return errors.New("no encryptor is available, refusing to write the password in clear text")
		}
		blob, err := protector.Protect(s.ProxyPassword)
		if err != nil {
			return fmt.Errorf("proxy password: %w", err)
		}
		stored.ProxySecret = blob
	}

	data, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("cannot create %s: %w", dir, err)
	}

	temp, err := os.CreateTemp(dir, ".config-*.tmp")
	if err != nil {
		return err
	}
	tempName := temp.Name()
	defer os.Remove(tempName) // no-op once the rename succeeded

	if _, err := temp.Write(data); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}
	return os.Rename(tempName, path)
}

// Backup moves an unreadable configuration out of the way and returns the new
// path, so the next Save does not destroy a file the user might want back.
func Backup(path string) (string, error) {
	if _, err := os.Stat(path); err != nil {
		return "", err
	}
	target := path + ".bak"
	_ = os.Remove(target)
	if err := os.Rename(path, target); err != nil {
		return "", err
	}
	return target, nil
}
