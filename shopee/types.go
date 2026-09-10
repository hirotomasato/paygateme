// Package shopee — domain types.
package shopee

// Cookie is a serializable cookie used by the fetch-only Shopee login flow.
type Cookie struct {
	Name     string `json:"name"`
	Value    string `json:"value"`
	Domain   string `json:"domain"`
	Path     string `json:"path"`
	Secure   bool   `json:"secure"`
	HTTPOnly bool   `json:"httpOnly"`
	Expires  int64  `json:"expires,omitempty"` // epoch ms; 0 = session
	HostOnly bool   `json:"hostOnly"`
}

// OtpRequestOptions configure the OTP request.
type OtpRequestOptions struct {
	Password     string // required for password-protected accounts
	Channel      int    // OTP delivery channel; 0 = default
	DeviceReport string // optional device-risk blob
	Language     string
	Timezone     string
}

// OtpChallenge is sensitive, short-lived state returned after an OTP is sent.
type OtpChallenge struct {
	Version           int      `json:"version"`
	PhoneNumber       string   `json:"phoneNumber"`
	Channel           int      `json:"channel"`
	AvailableChannels []int    `json:"availableChannels,omitempty"`
	DeviceFingerprint string   `json:"deviceFingerprint"`
	RiskToken         string   `json:"riskToken"`
	HasPassword       bool     `json:"hasPassword"`
	Cookies           []Cookie `json:"cookies"`
	RequestedAt       int64    `json:"requestedAt"`
}

// MerchantSummary is a merchant the account can access.
type MerchantSummary struct {
	ID                 string `json:"id"`
	Name               string `json:"name"`
	Status             int    `json:"status"`
	StaffUserID        int64  `json:"staffUserId"`
	StaffRole          int    `json:"staffRole"`
	StaffStatus        int    `json:"staffStatus"`
	IsActive           bool   `json:"isActive"`
	IsBanned           bool   `json:"isBanned"`
	IsCurrentLoginUser bool   `json:"isCurrentLoginUser"`
}

// OtpVerification is sensitive intermediate state after OTP verification.
type OtpVerification struct {
	Version           int               `json:"version"`
	TocNonce          string            `json:"tocNonce"`
	TocUserID         int64             `json:"tocUserId"`
	SPCClientID       string            `json:"spcClientId"`
	DeviceFingerprint string            `json:"deviceFingerprint"`
	Cookies           []Cookie          `json:"cookies"`
	Merchants         []MerchantSummary `json:"merchants"`
	VerifiedAt        int64             `json:"verifiedAt"`
}

// Store represents a Shopee store (outlet) under a merchant.
type Store struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Status int    `json:"status"`
}

// StaticQrisScope is owner metadata that prevents a manually supplied QRIS
// crossing stores.
type StaticQrisScope struct {
	MerchantID string `json:"merchantId"`
	StoreID    string `json:"storeId"`
}

// MerchantProfile is the full merchant profile including user info.
type MerchantProfile struct {
	MerchantID             string `json:"merchantId"`
	MerchantName           string `json:"merchantName"`
	StoreID                string `json:"storeId,omitempty"`
	AccountID              string `json:"accountId"`
	UserID                 string `json:"userId"`
	UserName               string `json:"userName"`
	Language               string `json:"language"`
	ShopeePayServiceStatus int    `json:"shopeePayServiceStatus"`
}

// Session is the persisted Shopee merchant session.
type Session struct {
	Version   int               `json:"version"`
	Cookies   []Cookie          `json:"cookies"`
	AccountID string            `json:"accountId"`
	Merchant  MerchantSummary   `json:"merchant"`
	Merchants []MerchantSummary `json:"merchants"`
	Stores    []Store           `json:"stores"`
	StoreID   string            `json:"storeId,omitempty"`
	CreatedAt int64             `json:"createdAt"`
	ExpiresAt int64             `json:"expiresAt,omitempty"`
	// SwitchCredential retains the account-session material needed to re-mint
	// the merchant token without a new OTP (refreshSession, selectMerchant).
	SwitchCredential *SwitchCredential `json:"switchCredential,omitempty"`
}

// SwitchCredential is the account-session material that lets a merchant token
// be re-minted through the SSO exchange without a fresh OTP.
type SwitchCredential struct {
	TocNonce          string `json:"tocNonce"`
	SPCClientID       string `json:"spcClientId"`
	DeviceFingerprint string `json:"deviceFingerprint"`
}

// VerifyOtpInput is the input to VerifyOtp.
type VerifyOtpInput struct {
	Challenge OtpChallenge
	OTP       string
}

// CompleteLoginInput is the input to CompleteLogin.
type CompleteLoginInput struct {
	Verification OtpVerification
	MerchantID   string
	StoreID      string // optional
}

// LoginWithOtpInput is the input to LoginWithOtp (combined verify + complete).
type LoginWithOtpInput struct {
	Challenge  OtpChallenge
	OTP        string
	MerchantID string // optional; auto-resolved when unambiguous
	StoreID    string // optional
}

// LoginStatus is the result of LoginWithOtp.
type LoginStatus string

const (
	LoginComplete                LoginStatus = "complete"
	LoginMerchantSelectionNeeded LoginStatus = "merchant-selection-required"
)

// LoginOutcome is the result of LoginWithOtp.
type LoginOutcome struct {
	Status       LoginStatus       `json:"status"`
	Session      *Session          `json:"session,omitempty"`
	Verification *OtpVerification  `json:"verification,omitempty"`
	Merchants    []MerchantSummary `json:"merchants,omitempty"`
}

// merchantCredential is the decoded dashboard JWT payload.
type merchantCredential struct {
	Token      string
	AccountID  string
	BusinessID string
	ExpiresAt  int64
}
