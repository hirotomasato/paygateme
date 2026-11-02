package gopay

import (
	"context"
	"testing"
	"time"

	"github.com/hirotomasato/paygateme/qris"
)

func buildValidStaticQRIS(t *testing.T) string {
	t.Helper()
	m := qris.EmvTlvMap{
		qris.TagPayloadFormat:     "01",
		qris.TagPointOfInitiation: qris.POIStatic,
		"26":                     "0012ID.CO.GOPAY.WWW0118936009110000000001",
		"52":                     "5912",
		"53":                     "360",
		"58":                     "ID",
		"59":                     "GOPAY STORE",
		"60":                     "Jakarta",
		"61":                     "12345",
	}
	payload, err := qris.BuildEmv(m)
	if err != nil {
		t.Fatalf("BuildEmv failed: %v", err)
	}
	return payload
}

func TestProviderNewAndSetStaticQRIS(t *testing.T) {
	validQR := buildValidStaticQRIS(t)

	provider := NewProvider(ProviderConfig{})
	if provider.Authenticated() {
		t.Errorf("expected new unauthenticated provider")
	}

	// Reject bad QRIS checksum
	if err := provider.SetStaticQRIS("000201010211BADCHECKSUM"); err == nil {
		t.Errorf("expected error for bad QRIS checksum")
	}

	// Accept valid QRIS
	if err := provider.SetStaticQRIS(validQR); err != nil {
		t.Errorf("expected SetStaticQRIS to succeed, got %v", err)
	}
	if provider.StaticQRIS() != validQR {
		t.Errorf("expected StaticQRIS() to match")
	}
}

func TestProviderPaymentsAndCreatePayment(t *testing.T) {
	validQR := buildValidStaticQRIS(t)

	session := SessionState{
		Tokens: TokenSet{
			AccessToken:  "mock-access-token",
			RefreshToken: "mock-refresh-token",
			ExpiresAt:    time.Now().Add(2 * time.Hour).UnixMilli(),
			TokenType:    "Bearer",
		},
		Merchant: &StoredMerchant{
			ID:           "m-100",
			MerchantName: "Toko Sukses",
			Outlets: []MerchantOutlet{
				{
					PopID:    "pop-100",
					Name:     "Kasir 1",
					QRString: validQR,
				},
			},
		},
		OutletID:   "pop-100",
		StaticQRIS: validQR,
	}

	provider := NewProvider(ProviderConfig{
		Session:    &session,
		StaticQRIS: validQR,
	})

	if !provider.Authenticated() {
		t.Errorf("expected provider to be authenticated with valid session")
	}

	// Scope
	scope := provider.GetPaymentScope()
	if scope == nil || scope.Provider != ProviderID || scope.AccountID != "m-100" || scope.MerchantID != "pop-100" {
		t.Errorf("unexpected payment scope: %+v", scope)
	}

	// Payments
	svc, err := provider.Payments()
	if err != nil {
		t.Fatalf("Payments() failed: %v", err)
	}
	if svc == nil {
		t.Fatalf("expected non-nil payment service")
	}

	// Create payment
	ctx := context.Background()
	p, err := provider.CreatePayment(ctx, 15000, "order-ref-001")
	if err != nil {
		t.Fatalf("CreatePayment failed: %v", err)
	}
	if p.BaseAmount != 15000 {
		t.Errorf("expected base amount 15000, got %d", p.BaseAmount)
	}
	if p.UniqueOffset <= 0 {
		t.Errorf("expected positive unique offset, got %d", p.UniqueOffset)
	}
	if p.UniqueAmount != p.BaseAmount+p.UniqueOffset {
		t.Errorf("expected UniqueAmount == BaseAmount + Offset, got %d", p.UniqueAmount)
	}
	if p.QRString == "" {
		t.Errorf("expected dynamic QRString to be generated")
	}

	// Cancel payment
	cancelled, err := provider.CancelPayment(ctx, p.ID)
	if err != nil {
		t.Fatalf("CancelPayment failed: %v", err)
	}
	if cancelled.Status != "cancelled" {
		t.Errorf("expected status cancelled, got %s", cancelled.Status)
	}
}

func TestProviderSessionExportRestore(t *testing.T) {
	initial := SessionState{
		Tokens: TokenSet{
			AccessToken:  "token-abc",
			RefreshToken: "refresh-xyz",
			ExpiresAt:    time.Now().Add(1 * time.Hour).UnixMilli(),
		},
		DeviceID: "custom-device-id",
	}

	provider := NewProvider(ProviderConfig{
		Session: &initial,
	})

	exported := provider.ExportSession()
	if exported == nil || exported.Tokens.AccessToken != "token-abc" {
		t.Errorf("unexpected exported session: %+v", exported)
	}

	provider2 := NewProvider(ProviderConfig{})
	if err := provider2.RestoreSession(*exported); err != nil {
		t.Fatalf("RestoreSession failed: %v", err)
	}
	if !provider2.Authenticated() {
		t.Errorf("expected restored provider to be authenticated")
	}
}

