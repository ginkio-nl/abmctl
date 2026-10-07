package config

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestConfig(t *testing.T, contents string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("writing test config: %v", err)
	}
	return path
}

func TestLoadMissingFileIsNotAnError(t *testing.T) {
	f, err := Load(filepath.Join(t.TempDir(), "does-not-exist.json"))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if f != nil {
		t.Fatal("expected a nil File for a missing config file")
	}
}

func TestLoadRejectsEmptyAccounts(t *testing.T) {
	path := writeTestConfig(t, `{"accounts": {}}`)
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for a config file with no accounts")
	}
}

func TestResolveSingleAccountNeedsNoName(t *testing.T) {
	path := writeTestConfig(t, `{
		"accounts": {
			"acme": {"client_id": "c1", "key_id": "k1", "private_key": "acme.pem"}
		}
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	acct, name, err := f.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if name != "acme" {
		t.Fatalf("name = %q, want acme", name)
	}
	if acct.ClientID != "c1" {
		t.Fatalf("ClientID = %q, want c1", acct.ClientID)
	}

	wantKeyPath := filepath.Join(filepath.Dir(path), "acme.pem")
	if acct.PrivateKeyPath != wantKeyPath {
		t.Fatalf("PrivateKeyPath = %q, want %q (relative to config dir)", acct.PrivateKeyPath, wantKeyPath)
	}
}

func TestResolveAmbiguousWithoutNameOrDefault(t *testing.T) {
	path := writeTestConfig(t, `{
		"accounts": {
			"acme": {"client_id": "c1", "key_id": "k1", "private_key": "acme.pem"},
			"globex": {"client_id": "c2", "key_id": "k2", "private_key": "globex.pem"}
		}
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, _, err := f.Resolve(""); err == nil {
		t.Fatal("expected an error when multiple accounts exist and none is named")
	}
}

func TestResolveUsesDefaultAccount(t *testing.T) {
	path := writeTestConfig(t, `{
		"default_account": "globex",
		"accounts": {
			"acme": {"client_id": "c1", "key_id": "k1", "private_key": "acme.pem"},
			"globex": {"client_id": "c2", "key_id": "k2", "private_key": "globex.pem"}
		}
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	acct, name, err := f.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if name != "globex" || acct.ClientID != "c2" {
		t.Fatalf("got (%q, %+v), want globex/c2", name, acct)
	}
}

func TestResolveExplicitNameOverridesDefault(t *testing.T) {
	path := writeTestConfig(t, `{
		"default_account": "globex",
		"accounts": {
			"acme": {"client_id": "c1", "key_id": "k1", "private_key": "acme.pem"},
			"globex": {"client_id": "c2", "key_id": "k2", "private_key": "globex.pem"}
		}
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	acct, name, err := f.Resolve("acme")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if name != "acme" || acct.ClientID != "c1" {
		t.Fatalf("got (%q, %+v), want acme/c1", name, acct)
	}
}

func TestResolveUnknownAccountName(t *testing.T) {
	path := writeTestConfig(t, `{
		"accounts": {
			"acme": {"client_id": "c1", "key_id": "k1", "private_key": "acme.pem"}
		}
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, _, err := f.Resolve("nope"); err == nil {
		t.Fatal("expected an error for an unknown account name")
	}
}

func TestListMarksDefaultAccount(t *testing.T) {
	path := writeTestConfig(t, `{
		"default_account": "globex",
		"accounts": {
			"acme": {"client_id": "c1", "key_id": "k1", "private_key": "acme.pem"},
			"globex": {"client_id": "c2", "key_id": "k2", "private_key": "globex.pem"}
		}
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	accounts := f.List()
	if len(accounts) != 2 {
		t.Fatalf("len(accounts) = %d, want 2", len(accounts))
	}
	// Sorted by name: acme, then globex.
	if accounts[0].Name != "acme" || accounts[0].IsDefault {
		t.Fatalf("accounts[0] = %+v, want acme/not-default", accounts[0])
	}
	if accounts[1].Name != "globex" || !accounts[1].IsDefault {
		t.Fatalf("accounts[1] = %+v, want globex/default", accounts[1])
	}
}

func TestListMarksSoleAccountAsImplicitDefault(t *testing.T) {
	path := writeTestConfig(t, `{
		"accounts": {
			"acme": {"client_id": "c1", "key_id": "k1", "private_key": "acme.pem"}
		}
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	accounts := f.List()
	if len(accounts) != 1 || !accounts[0].IsDefault {
		t.Fatalf("accounts = %+v, want a single implicit-default entry", accounts)
	}
}

func TestSetDefaultAndSaveRoundTrips(t *testing.T) {
	path := writeTestConfig(t, `{
		"accounts": {
			"acme": {"client_id": "c1", "key_id": "k1", "private_key": "acme.pem"},
			"globex": {"client_id": "c2", "key_id": "k2", "private_key": "globex.pem"}
		}
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := f.SetDefault("globex"); err != nil {
		t.Fatalf("SetDefault: %v", err)
	}
	if err := f.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded, err := Load(path)
	if err != nil {
		t.Fatalf("Load after Save: %v", err)
	}
	if reloaded.DefaultAccount != "globex" {
		t.Fatalf("DefaultAccount = %q, want globex", reloaded.DefaultAccount)
	}
	// Accounts (including the untouched relative private_key paths) must
	// survive the round trip.
	if reloaded.Accounts["acme"].ClientID != "c1" || reloaded.Accounts["globex"].ClientID != "c2" {
		t.Fatalf("accounts not preserved across Save/Load: %+v", reloaded.Accounts)
	}
}

func TestSetDefaultRejectsUnknownAccount(t *testing.T) {
	path := writeTestConfig(t, `{
		"accounts": {
			"acme": {"client_id": "c1", "key_id": "k1", "private_key": "acme.pem"}
		}
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	if err := f.SetDefault("nope"); err == nil {
		t.Fatal("expected an error for an unknown account name")
	}
	if f.DefaultAccount != "" {
		t.Fatalf("DefaultAccount = %q, want unchanged empty string after a rejected SetDefault", f.DefaultAccount)
	}
}

func TestResolveAbsolutePrivateKeyPathIsLeftAlone(t *testing.T) {
	path := writeTestConfig(t, `{
		"accounts": {
			"acme": {"client_id": "c1", "key_id": "k1", "private_key": "/abs/acme.pem"}
		}
	}`)
	f, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	acct, _, err := f.Resolve("")
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if acct.PrivateKeyPath != "/abs/acme.pem" {
		t.Fatalf("PrivateKeyPath = %q, want unchanged /abs/acme.pem", acct.PrivateKeyPath)
	}
}

func TestFilenameSafe(t *testing.T) {
	got := FilenameSafe("BUSINESSAPI.9703f56c-10ce/../weird id")
	for _, r := range got {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			// fine
		default:
			t.Fatalf("FilenameSafe produced an unsafe character %q in %q", r, got)
		}
	}
}
