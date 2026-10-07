// Package config loads abmctl's optional config file, which lets one file
// hold credentials for several named Apple Business Manager accounts as an
// alternative to always passing --client-id/--key-id/--private-key or
// setting ABM_* environment variables.
package config

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
)

// Account holds one Apple Business Manager account's credentials.
type Account struct {
	ClientID       string `json:"client_id"`
	TeamID         string `json:"team_id,omitempty"`
	KeyID          string `json:"key_id"`
	PrivateKeyPath string `json:"private_key"`
}

// File is the on-disk shape of the config file: a set of named accounts,
// plus which one to use when --account isn't given.
type File struct {
	DefaultAccount string             `json:"default_account,omitempty"`
	Accounts       map[string]Account `json:"accounts"`

	path string // set by Load; used to resolve relative private_key paths
}

// DefaultPath returns <user config dir>/abmctl/config.json -- e.g.
// ~/Library/Application Support/abmctl/config.json on macOS, or
// ~/.config/abmctl/config.json on Linux.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("config: locating user config dir: %w", err)
	}
	return filepath.Join(dir, "abmctl", "config.json"), nil
}

// SystemPath returns the machine-wide config file location, for a config
// deployed by an administrator or MDM rather than by each user:
// /Library/Application Support/abmctl/config.json on macOS,
// %ProgramData%\abmctl\config.json on Windows, and /etc/abmctl/config.json
// everywhere else.
func SystemPath() string {
	switch runtime.GOOS {
	case "darwin":
		return "/Library/Application Support/abmctl/config.json"
	case "windows":
		dir := os.Getenv("ProgramData")
		if dir == "" {
			dir = `C:\ProgramData`
		}
		return filepath.Join(dir, "abmctl", "config.json")
	default:
		return "/etc/abmctl/config.json"
	}
}

// DefaultPaths returns every location abmctl checks for a config file when
// --config isn't given, in priority order: the per-user DefaultPath first
// (so a user's own config always wins), then the machine-wide SystemPath.
func DefaultPaths() []string {
	var paths []string
	if p, err := DefaultPath(); err == nil {
		paths = append(paths, p)
	}
	return append(paths, SystemPath())
}

// FindDefault returns the first of DefaultPaths that exists and is
// readable, or "" if none is. An unreadable file is skipped rather than
// reported, so a system-wide config restricted to e.g. the admin group
// doesn't break abmctl for other users who pass credentials via flags or
// env vars instead.
func FindDefault() string {
	return firstReadable(DefaultPaths())
}

func firstReadable(paths []string) string {
	for _, p := range paths {
		f, err := os.Open(p)
		if err != nil {
			continue
		}
		f.Close()
		return p
	}
	return ""
}

// FilenameSafe keeps a Client ID readable in a filename (useful when
// debugging with --debug) while guaranteeing it's filesystem-safe. Used to
// name the per-account cache files next to the config file.
func FilenameSafe(s string) string {
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			return r
		default:
			return '_'
		}
	}, s)
}

// Load reads and parses the config file at path. A missing file is not an
// error: it returns (nil, nil) so callers can fall back to flags/env vars.
func Load(path string) (*File, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("config: reading %s: %w", path, err)
	}

	var f File
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, fmt.Errorf("config: parsing %s: %w", path, err)
	}
	if len(f.Accounts) == 0 {
		return nil, fmt.Errorf("config: %s defines no accounts", path)
	}
	f.path = path
	return &f, nil
}

// Resolve picks one account: by name if given, else the file's
// DefaultAccount, else the sole entry if there's exactly one. It's an
// error to leave the account ambiguous. Resolve also returns the resolved
// account's name (useful for --debug tracing) and rewrites a relative
// PrivateKeyPath to be relative to the config file's directory rather than
// the current working directory, so a config and its keys can live
// together and be referenced from anywhere.
func (f *File) Resolve(name string) (Account, string, error) {
	if name == "" {
		name = f.DefaultAccount
	}
	if name == "" {
		if len(f.Accounts) == 1 {
			for n := range f.Accounts {
				name = n
			}
		} else {
			return Account{}, "", fmt.Errorf("config: %s defines multiple accounts (%s); specify one with --account/ABM_ACCOUNT or set default_account", f.path, strings.Join(f.accountNames(), ", "))
		}
	}

	acct, ok := f.Accounts[name]
	if !ok {
		return Account{}, "", fmt.Errorf("config: no account %q in %s (known accounts: %s)", name, f.path, strings.Join(f.accountNames(), ", "))
	}

	if acct.PrivateKeyPath != "" && !filepath.IsAbs(acct.PrivateKeyPath) {
		acct.PrivateKeyPath = filepath.Join(filepath.Dir(f.path), acct.PrivateKeyPath)
	}

	return acct, name, nil
}

// AccountSummary is a credential-free view of one configured account, for
// listing (it deliberately omits PrivateKeyPath's resolved location and
// TeamID, which aren't useful for picking an account by name).
type AccountSummary struct {
	Name      string
	ClientID  string
	KeyID     string
	IsDefault bool
}

// List returns every account in the file, sorted by name, along with
// whether each is the one that Resolve("") would pick: the file's
// DefaultAccount, or its sole entry if there's exactly one account and no
// DefaultAccount is set.
func (f *File) List() []AccountSummary {
	def := f.DefaultAccount
	if def == "" && len(f.Accounts) == 1 {
		for n := range f.Accounts {
			def = n
		}
	}

	names := f.accountNames()
	out := make([]AccountSummary, len(names))
	for i, n := range names {
		a := f.Accounts[n]
		out[i] = AccountSummary{Name: n, ClientID: a.ClientID, KeyID: a.KeyID, IsDefault: n == def}
	}
	return out
}

// SetDefault updates the file's DefaultAccount to name, after checking
// that name refers to an account that actually exists. Call Save to
// persist the change.
func (f *File) SetDefault(name string) error {
	if _, ok := f.Accounts[name]; !ok {
		return fmt.Errorf("config: no account %q in %s (known accounts: %s)", name, f.path, strings.Join(f.accountNames(), ", "))
	}
	f.DefaultAccount = name
	return nil
}

// Save writes the file back to the path it was loaded from (via a temp
// file + rename, so a crash or a concurrent abmctl process never observes
// a half-written config file).
func (f *File) Save() error {
	raw, err := json.MarshalIndent(f, "", "  ")
	if err != nil {
		return fmt.Errorf("config: encoding %s: %w", f.path, err)
	}
	raw = append(raw, '\n')

	dir := filepath.Dir(f.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("config: creating %s: %w", dir, err)
	}

	tmp, err := os.CreateTemp(dir, "config-*.tmp")
	if err != nil {
		return fmt.Errorf("config: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath) // no-op once the rename below succeeds

	if _, err := tmp.Write(raw); err != nil {
		tmp.Close()
		return fmt.Errorf("config: writing %s: %w", tmpPath, err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := os.Chmod(tmpPath, 0o600); err != nil {
		return fmt.Errorf("config: %w", err)
	}
	if err := os.Rename(tmpPath, f.path); err != nil {
		return fmt.Errorf("config: renaming into place: %w", err)
	}
	return nil
}

func (f *File) accountNames() []string {
	names := make([]string, 0, len(f.Accounts))
	for n := range f.Accounts {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}
