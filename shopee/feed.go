package shopee

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/hirotomasato/paygateme/core"
	"github.com/hirotomasato/paygateme/utils"
)

// TransactionFeed implements core.TransactionFeed for Shopee's cursor-based
// payment feed. It owns pagination, status normalization, and the conversion
// from Shopee's Indonesian grouped format to whole rupiah.
type TransactionFeed struct {
	http       *HTTPClient
	token      string
	merchantID string
	storeID    string
	locale     APILocale
	logger     utils.Logger
}

// NewTransactionFeed creates a Shopee transaction feed.
func NewTransactionFeed(http *HTTPClient, token, merchantID, storeID string, locale APILocale, logger utils.Logger) *TransactionFeed {
	if logger == nil {
		logger = utils.NoopLogger
	}
	return &TransactionFeed{
		http:       http,
		token:      token,
		merchantID: merchantID,
		storeID:    storeID,
		locale:     locale,
		logger:     logger,
	}
}

// ListRecent implements core.TransactionFeed.
func (f *TransactionFeed) ListRecent(ctx context.Context, query core.TransactionFeedQuery) (core.TransactionFeedResult, error) {
	// Validate scope.
	if query.Scope.Provider != ProviderID ||
		query.Scope.MerchantID != f.storeID ||
		query.Scope.AccountID != f.merchantID {
		return core.TransactionFeedResult{}, core.NewConfigError(
			"Shopee transaction query scope does not match the session", nil)
	}

	pageSize := query.PageSize
	if pageSize <= 0 || pageSize > TransactionPageSize {
		pageSize = TransactionPageSize
	}
	maxPages := query.MaxPages
	if maxPages <= 0 {
		maxPages = 10
	}

	transactions := make([]core.MerchantTransaction, 0)
	seenIDs := make(map[string]bool)
	seenCursors := make(map[string]bool)
	var nextPosition string
	pagesFetched := 0

	for page := 0; page < maxPages; page++ {
		response := PaymentEnvelope{}
		if err := f.http.JSON(ctx, Request{
			URL:     ShopeeURL(PayBaseURL, EPTransactions),
			Method:  "POST",
			Headers: PaymentHeaders(),
			Body: map[string]any{
				"data": map[string]any{
					"metadata": PaymentMetadata(f.token, f.locale),
					"pageSize": pageSize,
					"filter": map[string]any{
						"startTime":   query.StartTime.Unix(),
						"endTime":     query.EndTime.Unix(),
						"serviceList": TransactionServices,
					},
					"sorter":        map[string]string{"field": "createTime", "order": "descend"},
					"next_position": nextPosition,
				},
			},
		}, &response); err != nil {
			return core.TransactionFeedResult{}, err
		}
		pagesFetched++

		data, err := RequirePaymentData(response, EPTransactions)
		if err != nil {
			return core.TransactionFeedResult{}, err
		}

		list, _ := data["list"].([]any)
		rejected := 0
		for _, raw := range list {
			rawMap, ok := raw.(map[string]any)
			if !ok {
				rejected++
				continue
			}
			tx := normalizeTransaction(rawMap, f.merchantID, f.storeID)
			if tx == nil {
				rejected++
				continue
			}
			if !seenIDs[tx.ID] {
				seenIDs[tx.ID] = true
				transactions = append(transactions, *tx)
			}
		}

		if rejected > 0 {
			f.logger.Warn("ignored malformed or out-of-scope Shopee transactions",
				map[string]any{"count": rejected, "provider": ProviderID})
		}

		cursor, _ := data["next_position"].(string)
		if cursor == "" {
			return core.TransactionFeedResult{Transactions: transactions, PagesFetched: pagesFetched, Truncated: false}, nil
		}
		if cursor == nextPosition || seenCursors[cursor] {
			return core.TransactionFeedResult{}, core.NewAPIError(
				"Shopee transaction cursor did not advance", "", nil)
		}
		seenCursors[cursor] = true
		nextPosition = cursor
	}

	return core.TransactionFeedResult{
		Transactions: transactions,
		PagesFetched: pagesFetched,
		Truncated:    nextPosition != "",
	}, nil
}

func normalizeTransaction(raw map[string]any, merchantID, storeID string) *core.MerchantTransaction {
	id, _ := raw["transactionId"].(string)
	id = trimSpace(id)
	if id == "" {
		return nil
	}

	amountStr, _ := raw["amount"].(string)
	amount, ok := ParseShopeeAmount(amountStr)
	if !ok {
		return nil
	}

	createTime := int64Value(raw, "createTime")
	if createTime == 0 {
		return nil
	}
	t := time.Unix(createTime, 0)

	rawMerchantID := jsonNumberToString(raw["merchantId"])
	rawStoreID := jsonNumberToString(raw["storeId"])
	if rawMerchantID != merchantID || rawStoreID != storeID {
		return nil
	}

	status := intValue(raw["status"], -1)
	completed := status == CompletedTransactionStatus
	statusLabel := "completed"
	if !completed {
		statusLabel = fmt.Sprintf("shopee:%d", status)
	}

	orderID, _ := raw["externalTransactionId"].(string)
	if orderID == "" {
		orderID, _ = raw["displayTransactionId"].(string)
	}
	if orderID == "" {
		orderID = id
	}

	paymentType := "shopee:" + jsonNumberToString(raw["service"])
	if raw["service"] == nil {
		paymentType = "shopee:" + jsonNumberToString(raw["transactionType"])
	}

	return &core.MerchantTransaction{
		ID:          id,
		OrderID:     orderID,
		Status:      statusLabel,
		PaymentType: paymentType,
		Amount:      amount,
		Currency:    "IDR",
		Time:        t,
		Raw:         raw,
	}
}

// jsonNumberToString renders a JSON value the way the reference client's
// String() does: numbers as plain integers (never scientific notation), so
// float64(17190191) becomes "17190191" — required for the scope check.
func jsonNumberToString(v any) string {
	switch t := v.(type) {
	case string:
		return t
	case float64:
		return strconv.FormatFloat(t, 'f', -1, 64)
	default:
		return fmt.Sprintf("%v", t)
	}
}
