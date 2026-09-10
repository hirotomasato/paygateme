package gopay

import (
	"context"
	"encoding/json"
)

// MerchantClient provides read access to merchant profile and account context
// via the GoBiz API.
type MerchantClient struct {
	http *HTTPClient
}

// NewMerchantClient creates a merchant client.
func NewMerchantClient(http *HTTPClient) *MerchantClient {
	return &MerchantClient{http: http}
}

// GetCurrentUser fetches the authenticated user from /v1/users/me.
func (c *MerchantClient) GetCurrentUser(ctx context.Context) (*CurrentUser, error) {
	var raw struct {
		User *struct {
			ID         int      `json:"id"`
			Email      string   `json:"email"`
			FullName   string   `json:"full_name"`
			Phone      string   `json:"phone"`
			MerchantID string   `json:"merchant_id"`
			Roles      []string `json:"roles"`
		} `json:"user"`
	}
	if err := c.http.RequestJSON(ctx, HTTPRequest{Method: "GET", Path: epUsersMe}, &raw); err != nil {
		return nil, err
	}
	user := raw.User
	if user == nil {
		user = &struct {
			ID         int      `json:"id"`
			Email      string   `json:"email"`
			FullName   string   `json:"full_name"`
			Phone      string   `json:"phone"`
			MerchantID string   `json:"merchant_id"`
			Roles      []string `json:"roles"`
		}{}
	}
	return &CurrentUser{
		MerchantID: user.MerchantID,
	}, nil
}

// GetMerchant fetches a merchant profile by id, including outlet QRIS.
func (c *MerchantClient) GetMerchant(ctx context.Context, merchantID string) (*MerchantProfile, error) {
	path := merchantDetail(merchantID)
	var raw struct {
		ID           string       `json:"id"`
		MerchantName string       `json:"merchant_name"`
		OutletName   string       `json:"outlet_name"`
		Phone        string       `json:"phone"`
		Email        string       `json:"email"`
		ServerKey    string       `json:"server_key"`
		ClientKey    string       `json:"client_key"`
		Timezone     string       `json:"timezone"`
		Pops         []popPayload `json:"pops"`
	}
	if err := c.http.RequestJSON(ctx, HTTPRequest{Method: "GET", Path: path}, &raw); err != nil {
		return nil, err
	}

	outlets := make([]MerchantOutlet, 0, len(raw.Pops))
	for _, p := range raw.Pops {
		outlets = append(outlets, normalizeOutlet(p))
	}
	rawJSON, _ := json.Marshal(raw)
	return &MerchantProfile{
		ID:           raw.ID,
		MerchantName: orDefault(raw.MerchantName, ""),
		OutletName:   raw.OutletName,
		Phone:        raw.Phone,
		Email:        raw.Email,
		ServerKey:    raw.ServerKey,
		ClientKey:    raw.ClientKey,
		Timezone:     raw.Timezone,
		Outlets:      outlets,
		Raw:          rawJSON,
	}, nil
}

// SearchMerchants enumerates all merchants the account can access via /v1/merchants/search.
func (c *MerchantClient) SearchMerchants(ctx context.Context, limit int) ([]StoredMerchant, error) {
	var raw struct {
		Success bool          `json:"success"`
		Total   int           `json:"total"`
		Hits    []merchantHit `json:"hits"`
	}
	body := map[string]any{
		"from":    0,
		"size":    limit,
		"_source": merchantSearchSource,
	}
	if err := c.http.RequestJSON(ctx, HTTPRequest{
		Method: "POST",
		Path:   epMerchantsSearch,
		Body:   body,
	}, &raw); err != nil {
		return nil, err
	}

	result := make([]StoredMerchant, 0, len(raw.Hits))
	for _, hit := range raw.Hits {
		result = append(result, normalizeStoredMerchant(hit))
	}
	return result, nil
}

// ---- Wire types ----

type popPayload struct {
	PopID  string        `json:"pop_id"`
	Name   string        `json:"name"`
	Status string        `json:"status"`
	GoPay  *gopayPayload `json:"gopay"`
}

type gopayPayload struct {
	ReceiverID string `json:"gopay_receiver_id"`
	GoPayQR    string `json:"gopay_qr_string"`
	ASPIQR     string `json:"aspi_qr_string"`
}

type merchantHit struct {
	ID           string       `json:"id"`
	MerchantName string       `json:"merchant_name"`
	OutletName   string       `json:"outlet_name"`
	Phone        string       `json:"phone"`
	Email        string       `json:"email"`
	BusinessType string       `json:"business_type"`
	MerchantType string       `json:"merchant_type"`
	ServiceArea  string       `json:"service_area"`
	Pops         []popPayload `json:"pops"`
}

// ---- Normalization ----

func normalizeOutlet(p popPayload) MerchantOutlet {
	o := MerchantOutlet{
		PopID:  orDefault(p.PopID, ""),
		Name:   p.Name,
		Status: p.Status,
		Raw:    mustMarshal(p),
	}
	if p.GoPay != nil {
		o.ReceiverID = p.GoPay.ReceiverID
		o.QRString = p.GoPay.ASPIQR
	}
	return o
}

func normalizeStoredMerchant(hit merchantHit) StoredMerchant {
	outlets := make([]MerchantOutlet, 0, len(hit.Pops))
	for _, p := range hit.Pops {
		outlets = append(outlets, normalizeOutlet(p))
	}
	var primaryQR string
	for _, o := range outlets {
		if o.QRString != "" {
			primaryQR = o.QRString
			break
		}
	}
	return StoredMerchant{
		ID:           hit.ID,
		MerchantName: orDefault(hit.MerchantName, ""),
		OutletName:   hit.OutletName,
		Phone:        hit.Phone,
		Email:        hit.Email,
		BusinessType: hit.BusinessType,
		MerchantType: hit.MerchantType,
		ServiceArea:  hit.ServiceArea,
		Outlets:      outlets,
		QRString:     primaryQR,
		Raw:          mustMarshal(hit),
	}
}

// merchantSearchSource mirrors the dashboard's requested fields.
var merchantSearchSource = []string{
	"id", "director_name", "merchant_name", "email", "feature_types",
	"phone", "outlet_address", "outlet_name", "outlet_city",
	"payment_settings.GOPAY", "tags", "bank_account", "applications",
	"pops", "aspi", "business_type", "metadata", "id_type",
	"merchant_type", "service_area",
}

func orDefault(s, def string) string {
	if s == "" {
		return def
	}
	return s
}

func mustMarshal(v any) json.RawMessage {
	data, _ := json.Marshal(v)
	return data
}
