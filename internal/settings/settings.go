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
//
// TODO(long-term): a custom binary format (.mct) was considered and deliberately
// deferred. JSON is inspectable, tolerates added fields and fails loudly; a hand
// written codec would add a length-prefixed parser with its own bounds checks,
// its own migration story and silent breakage, for a file of roughly 250 bytes
// that is read once per start. Revisit only if the file grows into something
// JSON handles badly (large history, binary payloads) or if it must be sealed as
// a whole.
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

// ResolvePath decides where the configuration lives.
//
// MCTUNNEL_CONFIG wins outright: an explicit request is never silently
// redirected, it is reported instead. Without it the first writable candidate
// wins: the roaming profile, then the folder next to the executable (portable
// installs, locked-down profiles), then the temp directory. The returned path is
// usable even when err is set, so the caller can still show where it tried.
func ResolvePath() (string, error) {
	if custom := strings.TrimSpace(os.Getenv(ConfigPathEnv)); custom != "" {
		if err := ensureWritable(filepath.Dir(custom)); err != nil {
			return custom, fmt.Errorf("%s points to %s: %w", ConfigPathEnv, custom, err)
		}
		return custom, nil
	}
	return resolveFrom(candidates())
}

// resolveFrom returns the first path whose directory can be created and written
// to, and reports why every candidate failed when none can.
func resolveFrom(paths []string) (string, error) {
	var problems []string
	for _, path := range paths {
		if err := ensureWritable(filepath.Dir(path)); err != nil {
			problems = append(problems, err.Error())
			continue
		}
		return path, nil
	}
	first := ""
	if len(paths) > 0 {
		first = paths[0]
	}
	return first, fmt.Errorf("no writable location for the configuration (%s)",
		strings.Join(problems, "; "))
}

// candidates lists the places the configuration may live, in order of
// preference.
func candidates() []string {
	var out []string
	if dir, err := os.UserConfigDir(); err == nil {
		out = append(out, filepath.Join(dir, "mctunnel", "config.json"))
	}
	if exe, err := os.Executable(); err == nil {
		out = append(out, filepath.Join(filepath.Dir(exe), "config.json"))
	}
	out = append(out, filepath.Join(os.TempDir(), "mctunnel", "config.json"))
	return out
}

// ensureWritable creates dir if needed and proves it is writable, which is the
// only reliable check on Windows: the ACL can deny what the path suggests.
func ensureWritable(dir string) error {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	probe, err := os.CreateTemp(dir, ".probe-*")
	if err != nil {
		return err
	}
	name := probe.Name()
	probe.Close()
	os.Remove(name)
	return nil
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
