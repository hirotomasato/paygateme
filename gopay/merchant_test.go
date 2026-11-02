package gopay

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalizeOutletAndMerchant(t *testing.T) {
	hit := merchantHit{
		ID:           "merchant-123",
		MerchantName: "Toko Enak",
		OutletName:   "Cabang Pusat",
		Phone:        "081234567890",
		Pops: []popPayload{
			{
				PopID:  "pop-1",
				Name:   "Kasir 1",
				Status: "active",
				GoPay: &gopayPayload{
					ReceiverID: "recv-01",
					ASPIQR:     "00020101021126610016ID.CO.SHOPEE.WWW...",
				},
			},
		},
	}

	m := normalizeStoredMerchant(hit)
	if m.ID != "merchant-123" {
		t.Errorf("expected ID merchant-123, got %s", m.ID)
	}
	if len(m.Outlets) != 1 {
		t.Fatalf("expected 1 outlet, got %d", len(m.Outlets))
	}
	if m.Outlets[0].PopID != "pop-1" {
		t.Errorf("expected popID pop-1, got %s", m.Outlets[0].PopID)
	}
	if m.QRString != "00020101021126610016ID.CO.SHOPEE.WWW..." {
		t.Errorf("expected primary QRString set from outlet, got %s", m.QRString)
	}
}

func TestMerchantClientSearchAndGet(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case epUsersMe:
			resp := map[string]any{
				"user": map[string]any{
					"id":          101,
					"merchant_id": "m-search-1",
					"email":       "merchant@example.com",
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		case epMerchantsSearch:
			resp := map[string]any{
				"success": true,
				"total":   1,
				"hits": []map[string]any{
					{
						"id":            "m-search-1",
						"merchant_name": "Warung Makmur",
						"pops": []map[string]any{
							{
								"pop_id": "pop-search-1",
								"name":   "Kasir Utama",
								"gopay": map[string]any{
									"aspi_qr_string": "00020101021126610016...",
								},
							},
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(resp)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	httpClient := NewHTTPClient(server.URL, nil)
	client := NewMerchantClient(httpClient)

	user, err := client.GetCurrentUser(context.Background())
	if err != nil {
		t.Fatalf("GetCurrentUser failed: %v", err)
	}
	if user.MerchantID != "m-search-1" {
		t.Errorf("expected MerchantID m-search-1, got %s", user.MerchantID)
	}

	merchants, err := client.SearchMerchants(context.Background(), 10)
	if err != nil {
		t.Fatalf("SearchMerchants failed: %v", err)
	}
	if len(merchants) != 1 {
		t.Fatalf("expected 1 merchant, got %d", len(merchants))
	}
	if merchants[0].MerchantName != "Warung Makmur" {
		t.Errorf("expected MerchantName Warung Makmur, got %s", merchants[0].MerchantName)
	}
}

