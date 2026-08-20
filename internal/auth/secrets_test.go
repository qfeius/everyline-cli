package auth

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"git.qtech.cn/ai/everyline-cli/internal/config"
)

func TestKeychainSecretStoreReadsAndWritesWithoutPuttingSecretInArgs(t *testing.T) {
	var savedArgs []string
	var savedInput string
	store := &KeychainSecretStore{
		service: "everyline-cli.test",
		run: func(_ context.Context, args []string, input io.Reader) ([]byte, error) {
			savedArgs = append([]string(nil), args...)
			if input != nil {
				content, err := io.ReadAll(input)
				if err != nil {
					return nil, err
				}
				savedInput = string(content)
			}
			if args[0] == "find-generic-password" {
				return []byte("stored-secret\n"), nil
			}
			return nil, nil
		},
	}

	if err := store.SaveAppSecret("dev", "stored-secret"); err != nil {
		t.Fatal(err)
	}
	if savedInput != "stored-secret\n" {
		t.Fatalf("security stdin=%q", savedInput)
	}
	if strings.Contains(strings.Join(savedArgs, " "), "stored-secret") {
		t.Fatalf("security args leaked secret: %#v", savedArgs)
	}
	secret, err := store.LoadAppSecret("dev")
	if err != nil || secret != "stored-secret" {
		t.Fatalf("secret=%q err=%v", secret, err)
	}
}

func TestFallbackSecretStoreLoadsFileWhenKeychainUnavailable(t *testing.T) {
	primary := &stubSecretStore{loadErr: errors.New("Keychain unavailable")}
	fileStore := NewFileSecretStore(filepath.Join(t.TempDir(), "secrets.json"))
	if err := fileStore.SaveAppSecret("dev", "file-secret"); err != nil {
		t.Fatal(err)
	}
	store := NewFallbackSecretStore(primary, fileStore)

	secret, err := store.LoadAppSecret("dev")
	if err != nil || secret != "file-secret" {
		t.Fatalf("secret=%q err=%v", secret, err)
	}
	if !primary.loadCalled {
		t.Fatal("expected Keychain primary to be attempted")
	}
}

func TestFallbackSecretStoreSavesFileWhenKeychainUnavailable(t *testing.T) {
	primary := &stubSecretStore{saveErr: errors.New("Keychain unavailable")}
	fileStore := NewFileSecretStore(filepath.Join(t.TempDir(), "secrets.json"))
	store := NewFallbackSecretStore(primary, fileStore)

	if err := store.SaveAppSecret("dev", "file-secret"); err != nil {
		t.Fatal(err)
	}
	if !primary.saveCalled {
		t.Fatal("expected Keychain primary to be attempted")
	}
	if secret, err := fileStore.LoadAppSecret("dev"); err != nil || secret != "file-secret" {
		t.Fatalf("file secret=%q err=%v", secret, err)
	}
}

type stubSecretStore struct {
	loadErr    error
	saveErr    error
	loadCalled bool
	saveCalled bool
}

func (store *stubSecretStore) LoadAppSecret(string) (string, error) {
	store.loadCalled = true
	if store.loadErr != nil {
		return "", store.loadErr
	}
	return "stub-secret", nil
}

func (store *stubSecretStore) SaveAppSecret(string, string) error {
	store.saveCalled = true
	return store.saveErr
}

func TestFileSecretStoreSavesProfilesWithPrivatePermissions(t *testing.T) {
	directory := t.TempDir()
	store := NewFileSecretStore(filepath.Join(directory, "secrets.json"))
	if err := store.SaveAppSecret("dev", "dev-secret"); err != nil {
		t.Fatal(err)
	}
	if err := store.SaveAppSecret("prod", "prod-secret"); err != nil {
		t.Fatal(err)
	}
	if got, err := store.LoadAppSecret("dev"); err != nil || got != "dev-secret" {
		t.Fatalf("dev secret=%q err=%v", got, err)
	}
	if got, err := store.LoadAppSecret("prod"); err != nil || got != "prod-secret" {
		t.Fatalf("prod secret=%q err=%v", got, err)
	}
	if _, err := store.LoadAppSecret("missing"); !errors.Is(err, ErrAppSecretNotFound) {
		t.Fatalf("missing secret err=%v", err)
	}
	fileInfo, err := os.Stat(filepath.Join(directory, "secrets.json"))
	if err != nil {
		t.Fatal(err)
	}
	if fileInfo.Mode().Perm() != 0o600 {
		t.Fatalf("secret file mode=%o, want 600", fileInfo.Mode().Perm())
	}
	directoryInfo, err := os.Stat(directory)
	if err != nil {
		t.Fatal(err)
	}
	if directoryInfo.Mode().Perm() != 0o700 {
		t.Fatalf("secret directory mode=%o, want 700", directoryInfo.Mode().Perm())
	}
}

func TestFileSecretStoreConcurrentInstancesPreserveProfiles(t *testing.T) {
	path := filepath.Join(t.TempDir(), "secrets.json")
	const count = 8
	var waitGroup sync.WaitGroup
	errorsChannel := make(chan error, count)
	for index := 0; index < count; index++ {
		index := index
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()
			store := NewFileSecretStore(path)
			errorsChannel <- store.SaveAppSecret("profile-"+string(rune('a'+index)), "secret-"+string(rune('a'+index)))
		}()
	}
	waitGroup.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		if err != nil {
			t.Fatal(err)
		}
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var stored appSecretFile
	if err := json.Unmarshal(content, &stored); err != nil {
		t.Fatal(err)
	}
	if len(stored.AppSecrets) != count {
		t.Fatalf("stored profiles=%#v", stored.AppSecrets)
	}
}

func TestProviderUsesSavedSecretAfterExpiredToken(t *testing.T) {
	t.Setenv("EVERYLINE_ACCESS_TOKEN", "")
	t.Setenv("EVERYLINE_APP_SECRET", "")
	t.Setenv("EVERYLINE_APP_SECRET_DEV_2DEU", "")
	fixedNow := time.Date(2026, 8, 20, 10, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var body map[string]string
		if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["appSecret"] != "stored-secret" {
			t.Fatalf("request body=%#v", body)
		}
		_, _ = writer.Write([]byte(`{"code":0,"msg":"ok","tenant_access_token":"fresh-token","expire":7200}`))
	}))
	defer server.Close()

	directory := t.TempDir()
	secretStore := NewFileSecretStore(filepath.Join(directory, "secrets.json"))
	if err := secretStore.SaveAppSecret("dev", "stored-secret"); err != nil {
		t.Fatal(err)
	}
	tokenStore := NewFileTokenStore(filepath.Join(directory, "tokens.json"))
	if err := tokenStore.Save("dev", Token{AccessToken: "expired-token", ExpiresAt: fixedNow.Add(-time.Minute)}); err != nil {
		t.Fatal(err)
	}
	provider := NewProvider(tokenStore, server.Client(), func() time.Time { return fixedNow }, secretStore)
	profile := config.Profile{Name: "dev", TokenURL: server.URL + "/token", AppID: "app"}

	token, err := provider.Token(context.Background(), profile)
	if err != nil {
		t.Fatal(err)
	}
	if token.AccessToken != "fresh-token" {
		t.Fatalf("token=%#v", token)
	}
}
