// Package shopee implements the Shopee Merchant / ShopeePay provider adapter.
package shopee

// ProviderID is the canonical provider identifier.
const ProviderID = "shopee"

// Base URLs.
const (
	AccountBaseURL       = "https://partner.business.accounts.shopee.co.id"
	PartnerBaseURL       = "https://partner.shopee.co.id"
	PartnerAPIBaseURL    = "https://api.partner.shopee.co.id"
	PayBaseURL           = "https://shopeepay.shopee.co.id"
	DeviceFingerprintURL = "https://df.infra.sz.shopee.co.id/v2/shpsec/web/report"
	SZSDKVersion         = "1.12.26-user.1"
)

// Endpoints.
const (
	EPCheckPasswordMigration = "/api/v4/account/business/check_password_migrate"
	EPCheckAccountByPassword = "/api/v4/account/business/check_account_exist_by_password"
	EPAuthenticateByPassword = "/api/v4/account/business/authenticate_toc_by_password"
	EPOtpSettings            = "/api/v4/account/business/get_otp_settings"
	EPSendOtp                = "/api/v4/account/business/send_otp"
	EPVerifyOtp              = "/api/v4/account/business/verify_otp"
	EPAuthenticateByOtp      = "/api/v4/account/business/authenticate_toc_by_otp"
	EPLoginToc               = "/api/v4/account/business/login_toc"
	EPLoginStatus            = "/api/v4/account/business/login_status"
	EPMerchantDetect         = "/nb/mss/mer-detect-api/PartnerMerchantDetectServer/MerchantDetect"
	EPSwitchMerchant         = "/nb/mss/mer-detect-api/PartnerMerchantDetectServer/SwitchMerchant"
	EPUserInfo               = "/nb/mss/web-api/PartnerAccountServer/GetUserInfo"
	EPAccountLogin           = "/account/login/auth"
	EPAccountLoginToken      = "/authenticate/login/token/"
	EPAccountTobAuth         = "/account/login/tob/auth"
	EPPartnerLoginAuth       = "/login/auth"
	EPStores                 = "/merchant/v1/partner-web/get-store-list"
	EPTransactions           = "/merchant/v1/partner-web/get-transaction-list"
)

// OTP constants.
const (
	OTPOperation      = 50001
	DefaultOTPChannel = 3 // WhatsApp
	DefaultLanguage   = "id"
	DefaultTimezone   = "Asia/Jakarta"
)

// Auth constants.
const (
	PartnerLoginFrom = "12"
	BusinessClientID = "1"
	AccountClientID  = "5"
)

// Transaction feed constants.
const (
	CompletedTransactionStatus = 3
	TransactionPageSize        = 10
	StorePageSize              = 30
)

// Cookie names.
const (
	LiveTokenCookie = "__shopee_partner_website_x_token_live"
	ClientIDCookie  = "SPC_CLIENTID"
)

// Auth error codes (passport).
const (
	AuthErrorNeedOTP  = 48401102
	AuthErrorNotLogin = 48500102
)

// Invalid token codes that signal the merchant token was rejected.
var InvalidTokenCodes = map[string]bool{
	"200020": true,
	"200021": true,
	"200022": true,
	"200023": true,
}

// Transaction services for the feed.
var TransactionServices = []int{1, 3}

// Store services.
var StoreServices = []int{1, 10}
