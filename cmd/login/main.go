// Command login is an interactive smoke test for the full Shopee flow:
// login -> store select -> QRIS bind -> payment create. Prints only
// non-sensitive output (never cookies, tokens, OTP, or raw QRIS payloads).
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/hirotomasato/paygateme/shopee"
	"github.com/hirotomasato/paygateme/utils"
)

func main() {
	savePath := flag.String("save", "", "path to persist the session JSON (sensitive; gitignore it)")
	restorePath := flag.String("restore", "", "path to a saved session JSON; skips login when readable")
	deviceReport := flag.String("device", shopee.DeviceRiskBlob, "device-risk blob")
	amount := flag.Int64("amount", 0, "create a payment with this base amount after login (0 = skip)")
	staticQris := flag.String("qris", "", "static QRIS payload to bind after login (empty = skip)")
	flag.Parse()

	ctx := context.Background()

	// Session restore: when a saved session is supplied and loads, skip the
	// OTP login entirely. Renewal happens through RefreshSession, not a new OTP.
	var restored *shopee.Session
	if *restorePath != "" {
		data, err := os.ReadFile(*restorePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "restore: %v\n", err)
			os.Exit(1)
		}
		var s shopee.Session
		if err := json.Unmarshal(data, &s); err != nil {
			fmt.Fprintf(os.Stderr, "restore: unreadable session JSON: %v\n", err)
			os.Exit(1)
		}
		restored = &s
	}

	provider := shopee.NewProvider(shopee.ProviderConfig{
		DeviceReport: *deviceReport,
		Session:      restored,
		Logger:       utils.NewConsoleLogger(utils.LevelInfo),
		OnSessionUpdated: func(s shopee.Session) error {
			if *savePath == "" {
				return nil
			}
			data, err := json.MarshalIndent(s, "", "  ")
			if err != nil {
				return err
			}
			return os.WriteFile(*savePath, data, 0o600)
		},
	})

	reader := bufio.NewReader(os.Stdin)

	if provider.Authenticated() {
		s := provider.ExportSession()
		fmt.Printf("restored session: merchant %s (id=%s), storeId=%s\n", s.Merchant.Name, s.Merchant.ID, s.StoreID)
	} else {
		if err := runLogin(ctx, provider, reader); err != nil {
			fmt.Fprintf(os.Stderr, "login failed: %v\n", err)
			os.Exit(1)
		}
	}

	// Store selection (auto-selects when exactly one store).
	session := provider.ExportSession()
	if session == nil {
		fmt.Fprintln(os.Stderr, "no session after login")
		os.Exit(1)
	}
	if session.StoreID == "" {
		stores, err := provider.ListStores(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "listStores failed: %v\n", err)
			os.Exit(1)
		}
		if len(stores) == 0 {
			fmt.Fprintln(os.Stderr, "no stores accessible for this merchant")
			os.Exit(1)
		}
		fmt.Println("stores:")
		for i, s := range stores {
			fmt.Printf("  [%d] %s (id=%s)\n", i, s.Name, s.ID)
		}
		choice := "0"
		if len(stores) > 1 {
			choice = prompt(reader, "store index: ")
		}
		idx, err := strconv.Atoi(strings.TrimSpace(choice))
		if err != nil || idx < 0 || idx >= len(stores) {
			fmt.Fprintln(os.Stderr, "invalid choice")
			os.Exit(1)
		}
		if _, err := provider.SelectStore(ctx, stores[idx].ID); err != nil {
			fmt.Fprintf(os.Stderr, "selectStore failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("store selected: %s (id=%s)\n", stores[idx].Name, stores[idx].ID)
	}

	// Session renewal check: refresh silently when possible.
	if _, err := provider.RefreshSession(ctx); err != nil {
		fmt.Printf("session refresh skipped: %v\n", err)
	} else {
		fmt.Println("session refreshed (token re-minted, no OTP)")
	}

	// QRIS binding.
	if *staticQris != "" {
		if err := provider.SetStaticQris(*staticQris); err != nil {
			fmt.Fprintf(os.Stderr, "setStaticQris failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("static QRIS bound")
	}

	// Payment creation.
	if *amount > 0 {
		payment, err := provider.CreatePayment(ctx, *amount, "debug-order-1")
		if err != nil {
			fmt.Fprintf(os.Stderr, "createPayment failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("\npayment created:\n")
		fmt.Printf("  id:            %s\n", payment.ID)
		fmt.Printf("  base amount:   %d\n", payment.BaseAmount)
		fmt.Printf("  unique amount: %d (offset %d)\n", payment.UniqueAmount, payment.UniqueOffset)
		if payment.QRString != "" {
			fmt.Printf("  dynamic QRIS: generated (%d bytes) — render it locally, do not log\n", len(payment.QRString))
		} else {
			fmt.Println("  dynamic QRIS: (no static QRIS bound)")
		}
	}
}

func runLogin(ctx context.Context, provider *shopee.Provider, reader *bufio.Reader) error {
	phone := prompt(reader, "phone (e.g. 0812xxxx or +62...): ")
	password := prompt(reader, "password (empty if passwordless): ")

	challenge, err := provider.RequestOtp(ctx, phone, shopee.OtpRequestOptions{Password: password})
	if err != nil {
		return fmt.Errorf("requestOtp: %w", err)
	}
	fmt.Printf("OTP requested (channel %d, hasPassword=%v). Check your phone.\n",
		challenge.Channel, challenge.HasPassword)

	otp := prompt(reader, "OTP code: ")

	outcome, err := provider.LoginWithOtp(ctx, shopee.LoginWithOtpInput{
		Challenge: *challenge,
		OTP:       otp,
	})
	if err != nil {
		return fmt.Errorf("loginWithOtp: %w", err)
	}

	if outcome.Status == shopee.LoginMerchantSelectionNeeded {
		fmt.Println("Multiple merchants accessible; pick one:")
		for i, m := range outcome.Merchants {
			fmt.Printf("  [%d] %s (id=%s)\n", i, m.Name, m.ID)
		}
		choice := prompt(reader, "merchant index: ")
		idx, err := strconv.Atoi(strings.TrimSpace(choice))
		if err != nil || idx < 0 || idx >= len(outcome.Merchants) {
			return fmt.Errorf("invalid merchant choice")
		}
		if _, err := provider.CompleteLogin(ctx, shopee.CompleteLoginInput{
			Verification: *outcome.Verification,
			MerchantID:   outcome.Merchants[idx].ID,
		}); err != nil {
			return fmt.Errorf("completeLogin: %w", err)
		}
	}

	session := provider.ExportSession()
	if session == nil {
		return fmt.Errorf("no session after login")
	}
	fmt.Printf("\nlogin OK: merchant %s (id=%s)\n", session.Merchant.Name, session.Merchant.ID)
	return nil
}

func prompt(r *bufio.Reader, label string) string {
	fmt.Print(label)
	s, _ := r.ReadString('\n')
	return strings.TrimSpace(s)
}
