package service

import (
	"context"
	"shortlink/internal/auth"
	"shortlink/internal/crypto"
	"shortlink/internal/model"
	"shortlink/internal/repository"
	"strings"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func testAPIKeyService(t *testing.T) (*APIKeyService, *repository.APIKeyRepository) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := db.AutoMigrate(&model.APIKey{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	repo := repository.NewAPIKeyRepository(db)
	strong := crypto.NewStrongCrypto(crypto.MustGetKey("test-encryption-key"))
	weak := crypto.NewWeakCrypto("test-encryption-key")
	return NewAPIKeyService(repo, nil, crypto.NewCryptoManager(strong, weak)), repo
}

func TestAPIKeyRevealRoundTrip(t *testing.T) {
	svc, _ := testAPIKeyService(t)
	plain, key, err := svc.CreateWithOptions(context.Background(), APIKeyOptions{Name: "ci"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if key.KeyHash == "" || key.KeyEnc == "" || key.KeyFingerprint == "" || key.KeyPrefix == "" {
		t.Fatalf("missing stored key metadata: %#v", key)
	}
	if strings.Contains(key.KeyEnc, plain) {
		t.Fatal("encrypted key contains plaintext")
	}
	revealed, revealedKey, err := svc.Reveal(context.Background(), key.ID)
	if err != nil {
		t.Fatalf("reveal: %v", err)
	}
	if revealed != plain || revealedKey.ID != key.ID {
		t.Fatalf("revealed=%q id=%d want plain and id %d", revealed, revealedKey.ID, key.ID)
	}
	validated, err := svc.Validate(context.Background(), plain)
	if err != nil {
		t.Fatalf("validate: %v", err)
	}
	if validated.ID != key.ID {
		t.Fatalf("validated id=%d want %d", validated.ID, key.ID)
	}
}

func TestAPIKeyRevealRejectsLegacyAndTampered(t *testing.T) {
	svc, repo := testAPIKeyService(t)
	legacyPlain := "sl_legacy"
	legacy := &model.APIKey{KeyHash: auth.HashAPIKey(legacyPlain), Name: "legacy"}
	if err := repo.Create(context.Background(), legacy); err != nil {
		t.Fatalf("create legacy: %v", err)
	}
	if _, _, err := svc.Reveal(context.Background(), legacy.ID); err == nil || !strings.Contains(err.Error(), "legacy") {
		t.Fatalf("expected legacy reveal error, got %v", err)
	}

	plain, key, err := svc.CreateWithOptions(context.Background(), APIKeyOptions{Name: "tampered"})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if err := repo.Update(context.Background(), key.ID, map[string]interface{}{"key_hash": auth.HashAPIKey(plain + "x")}); err != nil {
		t.Fatalf("tamper: %v", err)
	}
	if _, _, err := svc.Reveal(context.Background(), key.ID); err == nil || !strings.Contains(err.Error(), "integrity") {
		t.Fatalf("expected integrity error, got %v", err)
	}
}
