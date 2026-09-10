package shopee

import (
	"context"
	"fmt"

	"github.com/hirotomasato/paygateme/core"
	"github.com/hirotomasato/paygateme/utils"
)

// MerchantClient provides merchant profile and store discovery for an
// authenticated session.
type MerchantClient struct {
	http       *HTTPClient
	token      string
	merchantID string
	locale     APILocale
	logger     utils.Logger
}

// NewMerchantClient creates a Shopee merchant client.
func NewMerchantClient(http *HTTPClient, token, merchantID string, locale APILocale, logger utils.Logger) *MerchantClient {
	if logger == nil {
		logger = utils.NoopLogger
	}
	return &MerchantClient{
		http:       http,
		token:      token,
		merchantID: merchantID,
		locale:     locale,
		logger:     logger,
	}
}

// GetProfile returns the full merchant profile.
func (m *MerchantClient) GetProfile(ctx context.Context) (*MerchantProfile, error) {
	response := PartnerEnvelope{}
	if err := m.http.JSON(ctx, Request{
		URL:     ShopeeURL(PartnerAPIBaseURL, EPUserInfo),
		Method:  "POST",
		Headers: PartnerHeaders(PartnerHeaderOpts{Token: m.token, Locale: m.locale}),
		Body:    map[string]any{},
	}, &response); err != nil {
		return nil, err
	}
	raw, err := RequirePartnerData(response, EPUserInfo)
	if err != nil {
		return nil, err
	}

	merchantID := toString(raw["merchantId"])
	if merchantID == "" || merchantID != m.merchantID {
		return nil, core.NewAPIError("Shopee returned a profile for a different merchant", "", nil)
	}

	return &MerchantProfile{
		MerchantID:             merchantID,
		MerchantName:           stringValue(raw["merchantName"]),
		StoreID:                toString(raw["store_id"]),
		AccountID:              toString(raw["tocUid"]),
		UserID:                 toString(raw["tobUserId"]),
		UserName:               defaultString(stringValue(raw["userName"]), stringValue(raw["tocUserName"])),
		Language:               stringValue(raw["language"]),
		ShopeePayServiceStatus: int(intValue(raw["shopeepay_service_status"], 0)),
	}, nil
}

// ListStores returns every store the active merchant owns.
func (m *MerchantClient) ListStores(ctx context.Context, maxPages int) ([]Store, error) {
	if maxPages <= 0 {
		maxPages = 100
	}
	filtered, err := m.fetchStores(ctx, maxPages, StoreServices)
	if err != nil {
		return nil, err
	}
	if len(filtered) > 0 {
		return filtered, nil
	}
	// Retry unfiltered for merchants whose stores carry neither service.
	return m.fetchStores(ctx, maxPages, nil)
}

func (m *MerchantClient) fetchStores(ctx context.Context, maxPages int, serviceList []int) ([]Store, error) {
	stores := make(map[string]Store)
	seenCursors := map[int64]bool{0: true}
	var lastStoreID int64

	for page := 0; page < maxPages; page++ {
		body := map[string]any{
			"metadata":    PaymentMetadata(m.token, m.locale),
			"storeName":   "",
			"lastStoreId": lastStoreID,
			"pageSize":    StorePageSize,
		}
		if serviceList != nil {
			body["serviceList"] = serviceList
		}

		response := PaymentEnvelope{}
		if err := m.http.JSON(ctx, Request{
			URL:     ShopeeURL(PayBaseURL, EPStores),
			Method:  "POST",
			Headers: PaymentHeaders(),
			Body:    map[string]any{"data": body},
		}, &response); err != nil {
			return nil, err
		}
		data, err := RequirePaymentData(response, EPStores)
		if err != nil {
			return nil, err
		}

		list, _ := data["list"].([]any)
		for _, raw := range list {
			rawMap, ok := raw.(map[string]any)
			if !ok {
				continue
			}
			id := toString(rawMap["storeId"])
			if id == "" {
				continue
			}
			stores[id] = Store{
				ID:     id,
				Name:   stringValue(rawMap["storeName"]),
				Status: int(intValue(rawMap["status"], 0)),
			}
		}

		totalCount := intValue(data["storeCount"], -1)
		if len(list) == 0 || len(list) < StorePageSize || (totalCount >= 0 && len(stores) >= totalCount) {
			break
		}

		if len(list) == 0 {
			break
		}
		lastRaw, ok := list[len(list)-1].(map[string]any)
		if !ok {
			break
		}
		nextID := int64Value(lastRaw, "storeId")
		if nextID == 0 || seenCursors[nextID] {
			return nil, core.NewAPIError("Shopee store cursor did not advance", "", nil)
		}
		seenCursors[nextID] = true
		lastStoreID = nextID
	}

	out := make([]Store, 0, len(stores))
	for _, s := range stores {
		out = append(out, s)
	}
	return out, nil
}

func toString(v any) string {
	switch t := v.(type) {
	case string:
		if t != "" {
			return t
		}
	case float64:
		return fmt.Sprintf("%d", int64(t))
	case int64:
		return fmt.Sprintf("%d", t)
	}
	return ""
}
