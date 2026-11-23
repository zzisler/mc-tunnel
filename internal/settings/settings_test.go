package settings

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeProtector stands in for DPAPI: reversible, but not the plaintext.
type fakeProtector struct{ fail bool }

func (p fakeProtector) Protect(plaintext string) (string, error) {
	if p.fail {
		return "", errFake
	}
	return "enc:" + base64.StdEncoding.EncodeToString([]byte(plaintext)), nil
}

func (p fakeProtector) Unprotect(blob string) (string, error) {
	if p.fail {
		return "", errFake
	}
	if !strings.HasPrefix(blob, "enc:") {
		return "", errFake
	}
	raw, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(blob, "enc:"))
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func TestRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config.json")
	want := Settings{
		Server:        "mc.example.net:25565",
		ProxyKind:     "http",
		ProxyAddr:     "198.51.100.7:39584",
		ProxyUser:     "user",
		ProxyPassword: "super-secret",
		LocalPort:     25570,
		LogsVisible:   true,
	}
	if err := Save(path, want, fakeProtector{}); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := Load(path, fakeProtector{})
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got != want {
		t.Fatalf("round trip\n got %+v\nwant %+v", got, want)
	}
}

// The whole point of the Protector: the file must not contain the password.
func TestPasswordIsNotStoredInClear(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	err := Save(path, Settings{ProxyPassword: "super-secret", LocalPort: 25565}, fakeProtector{})
	if err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "super-secret") {
		t.Fatalf("the password is in the file:\n%s", raw)
	}
	if !strings.Contains(string(raw), `"proxy_secret"`) {
		t.Fatalf("no secret field in the file:\n%s", raw)
	}
}

func TestMissingFileGivesDefaults(t *testing.T) {
	got, err := Load(filepath.Join(t.TempDir(), "absent.json"), fakeProtector{})
	if err != nil {
		t.Fatalf("a missing file must not be an error: %v", err)
	}
	if got != Default() {
		t.Fatalf("got %+v, want %+v", got, Default())
	}
}

func TestCorruptFileIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, fakeProtector{}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestUnknownVersionIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"version":99,"server":"x"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, fakeProtector{}); err == nil {
		t.Fatal("expected an error")
	}
}

func TestBackupKeepsTheOldFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := os.WriteFile(path, []byte("garbage"), 0o600); err != nil {
		t.Fatal(err)
	}
	backup, err := Backup(path)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(backup); err != nil {
		t.Fatalf("backup missing: %v", err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatal("the original file is still there")
	}
}

func TestBackupWithoutFile(t *testing.T) {
	if _, err := Backup(filepath.Join(t.TempDir(), "absent.json")); err == nil {
		t.Fatal("expected an error when there is nothing to back up")
	}
}

func TestSaveRefusesToLeakWithoutProtector(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	err := Save(path, Settings{ProxyPassword: "secret", LocalPort: 25565}, nil)
	if err == nil {
		t.Fatal("writing a password without an encryptor must fail")
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatal("nothing should have been written")
	}
}

func TestSaveWithoutPassword(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Settings{Server: "srv", LocalPort: 25565}, nil); err != nil {
		t.Fatalf("saving without a password must work: %v", err)
	}
	got, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Server != "srv" {
		t.Fatalf("server = %q", got.Server)
	}
}

func TestProtectorFailureIsReported(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := Save(path, Settings{ProxyPassword: "secret", LocalPort: 25565}, fakeProtector{fail: true}); err == nil {
		t.Fatal("a failing protector must fail the save")
	}
}

func TestSaveIsAtomicAndLeavesNoTempFiles(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.json")
	if err := Save(path, Settings{Server: "first", LocalPort: 25565}, nil); err != nil {
		t.Fatal(err)
	}
	if err := Save(path, Settings{Server: "second", LocalPort: 25565}, nil); err != nil {
		t.Fatal(err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		names := make([]string, 0, len(entries))
		for _, entry := range entries {
			names = append(names, entry.Name())
		}
		t.Fatalf("temporary files left behind: %v", names)
	}
	got, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.Server != "second" {
		t.Fatalf("server = %q, want the last save", got.Server)
	}
}

func TestPortOutOfRangeFallsBackToDefault(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"version":1,"local_port":99999}`), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got.LocalPort != DefaultPort {
		t.Fatalf("local_port = %d, want %d", got.LocalPort, DefaultPort)
	}
}

func TestDefaultPathHonoursOverride(t *testing.T) {
	want := filepath.Join(t.TempDir(), "portable.json")
	t.Setenv(ConfigPathEnv, want)
	got, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("DefaultPath() = %q, want %q", got, want)
	}

	t.Setenv(ConfigPathEnv, "  ")
	fallback, err := DefaultPath()
	if err != nil {
		t.Fatal(err)
	}
	if fallback == want || !strings.HasSuffix(fallback, filepath.Join("mctunnel", "config.json")) {
		t.Fatalf("blank override should fall back to the user config dir, got %q", fallback)
	}
}

// Notepad saves UTF-8 with a BOM; a hand-edited config must still load.
func TestLoadAcceptsBOM(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	body := []byte(`{"version":1,"server":"mc.example.net","local_port":25570,"logs_visible":true}`)
	content := append([]byte{0xEF, 0xBB, 0xBF}, body...)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := Load(path, nil)
	if err != nil {
		t.Fatalf("a BOM must not break loading: %v", err)
	}
	if got.Server != "mc.example.net" || got.LocalPort != 25570 || !got.LogsVisible {
		t.Fatalf("got %+v", got)
	}
}

var errFake = errorString("fake protector failure")

type errorString string

func (e errorString) Error() string { return string(e) }
