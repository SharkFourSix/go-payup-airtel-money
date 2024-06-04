package gopayupairtelmoney_test

import (
	"context"
	"testing"
	"time"

	gopayup "github.com/sharkfoursix/go-payup"
	"github.com/sharkfoursix/go-payup/pkg"
	"gopkg.in/ini.v1"
)

func failIfError(t *testing.T, err error) {
	if err != nil {
		t.Error(err)
		t.FailNow()
		return
	}
}

func Test(t *testing.T) {
	var (
		err           error
		dsn           string
		cfg           *ini.File
		transactionID string
		wallet        pkg.MobileWallet
		transaction   pkg.Transaction
	)
	cfg, err = ini.Load(".ini")
	failIfError(t, err)

	sec := cfg.Section("airtel_money")
	dsn = sec.Key("dsn").String()
	transactionID = sec.Key("txn_id").String()

	wallet, err = gopayup.NewMobileWallet("airtelMoney", dsn)
	failIfError(t, err)

	ctx, cancelFn := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancelFn()

	transaction, err = wallet.VerifyTransaction(ctx, transactionID)
	failIfError(t, err)

	t.Log(transaction)
}
