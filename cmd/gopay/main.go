// Command gopay is an interactive CLI smoke test for the GoPay/GoBiz provider flow:
// login -> merchant/outlet select -> QRIS bind -> payment create.
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

	"github.com/hirotomasato/paygateme/gopay"
	"github.com/hirotomasato/paygateme/utils"
)

func main() {
	savePath := flag.String("save", "", "path to persist session (sensitive; gitignore it)")
	restorePath := flag.String("restore", "", "path to saved session; skips login when readable")
	passphrase := flag.String("passphrase", "", "optional passphrase to encrypt/decrypt session file at rest")
	amount := flag.Int64("amount", 0, "create a payment with this base amount after login (0 = skip)")
	staticQRIS := flag.String("qris", "", "static QRIS payload to bind after login (empty = use outlet QRIS)")
	flag.Parse()

	ctx := context.Background()

	var restored *gopay.SessionState
	if *restorePath != "" {
		data, err := os.ReadFile(*restorePath)
		if err != nil {
			fmt.Fprintf(os.Stderr, "restore: %v\n", err)
			os.Exit(1)
		}
		if *passphrase != "" {
			decrypted, err := utils.Decrypt(data, *passphrase)
			if err != nil {
				fmt.Fprintf(os.Stderr, "restore: decrypt session failed: %v\n", err)
				os.Exit(1)
			}
			data = decrypted
		}
		var s gopay.SessionState
		if err := json.Unmarshal(data, &s); err != nil {
			fmt.Fprintf(os.Stderr, "restore: unreadable session JSON: %v (if encrypted, pass -passphrase)\n", err)
			os.Exit(1)
		}
		restored = &s
	}

	provider := gopay.NewProvider(gopay.ProviderConfig{
		Session:    restored,
		StaticQRIS: *staticQRIS,
		Logger:     utils.NewConsoleLogger(utils.LevelInfo),
		OnSessionUpdated: func(s gopay.SessionState) error {
			if *savePath == "" {
				return nil
			}
			data, err := json.MarshalIndent(s, "", "  ")
			if err != nil {
				return err
			}
			if *passphrase != "" {
				encrypted, err := utils.Encrypt(data, *passphrase)
				if err != nil {
					return fmt.Errorf("encrypt session: %w", err)
				}
				data = encrypted
			}
			return os.WriteFile(*savePath, data, 0o600)
		},
	})

	reader := bufio.NewReader(os.Stdin)

	if provider.Authenticated() {
		s := provider.ExportSession()
		mName := "unnamed"
		if s.Merchant != nil {
			mName = s.Merchant.MerchantName
		}
		fmt.Printf("Restored GoPay session: merchant %s (outletId=%s)\n", mName, s.OutletID)
	} else {
		if err := runGoPayLogin(ctx, provider, reader); err != nil {
			fmt.Fprintf(os.Stderr, "login failed: %v\n", err)
			os.Exit(1)
		}
	}

	session := provider.ExportSession()
	if session == nil {
		fmt.Fprintln(os.Stderr, "no session after login")
		os.Exit(1)
	}

	// Select merchant if not already selected
	if session.Merchant == nil {
		merchants, err := provider.ListMerchants(ctx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "listMerchants failed: %v\n", err)
			os.Exit(1)
		}
		if len(merchants) == 0 {
			fmt.Fprintln(os.Stderr, "no GoBiz merchants accessible on this account")
			os.Exit(1)
		}

		fmt.Println("Available GoBiz merchants:")
		for i, m := range merchants {
			fmt.Printf("  [%d] %s (id=%s, outlets=%d)\n", i, m.MerchantName, m.ID, len(m.Outlets))
		}

		choice := "0"
		if len(merchants) > 1 {
			choice = prompt(reader, "Select merchant index: ")
		}
		idx, err := strconv.Atoi(strings.TrimSpace(choice))
		if err != nil || idx < 0 || idx >= len(merchants) {
			fmt.Fprintln(os.Stderr, "invalid merchant selection")
			os.Exit(1)
		}

		selected, err := provider.SelectMerchant(ctx, merchants[idx].ID)
		if err != nil {
			fmt.Fprintf(os.Stderr, "selectMerchant failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Printf("Selected merchant: %s\n", selected.MerchantName)
	}

	// Bind QRIS if supplied
	if *staticQRIS != "" {
		if err := provider.SetStaticQRIS(*staticQRIS); err != nil {
			fmt.Fprintf(os.Stderr, "bind static QRIS failed: %v\n", err)
			os.Exit(1)
		}
		fmt.Println("Bound manual static QRIS successfully.")
	} else if provider.StaticQRIS() != "" {
		fmt.Println("Using auto-discovered outlet static QRIS.")
	} else {
		fmt.Println("⚠️ No static QRIS bound yet. You can bind one using -qris <payload>.")
	}

	// Payment smoke test
	if *amount > 0 {
		if provider.StaticQRIS() == "" {
			fmt.Fprintln(os.Stderr, "cannot create payment: no static QRIS bound")
			os.Exit(1)
		}

		p, err := provider.CreatePayment(ctx, *amount, "cli-test")
		if err != nil {
			fmt.Fprintf(os.Stderr, "createPayment failed: %v\n", err)
			os.Exit(1)
		}

		fmt.Printf("Created payment: ID=%s, BaseAmount=%d, UniqueAmount=%d\n",
			p.ID, p.BaseAmount, p.UniqueAmount)
		fmt.Printf("Dynamic QRIS Payload: %s\n", p.QRString)
	}

	if *savePath != "" {
		sess := provider.ExportSession()
		data, _ := json.MarshalIndent(sess, "", "  ")
		if *passphrase != "" {
			if encrypted, err := utils.Encrypt(data, *passphrase); err == nil {
				data = encrypted
			}
		}
		_ = os.WriteFile(*savePath, data, 0o600)
		fmt.Printf("Session saved to %s\n", *savePath)
	}
}

func runGoPayLogin(ctx context.Context, p *gopay.Provider, reader *bufio.Reader) error {
	phone := prompt(reader, "GoPay/GoBiz phone number (e.g. 081234567890): ")
	phone = strings.TrimSpace(phone)

	reqResult, err := p.RequestOTP(ctx, phone, "62")
	if err != nil {
		return fmt.Errorf("request OTP: %w", err)
	}

	otp := prompt(reader, "Enter OTP received: ")
	otp = strings.TrimSpace(otp)

	sess, err := p.VerifyOTP(ctx, otp, reqResult.OTPToken)
	if err != nil {
		return fmt.Errorf("verify OTP: %w", err)
	}

	mName := "none"
	if sess.Merchant != nil {
		mName = sess.Merchant.MerchantName
	}
	fmt.Printf("Login successful! Active Merchant: %s\n", mName)
	return nil
}

func prompt(r *bufio.Reader, message string) string {
	fmt.Print(message)
	line, _ := r.ReadString('\n')
	return strings.TrimRight(line, "\r\n")
}

