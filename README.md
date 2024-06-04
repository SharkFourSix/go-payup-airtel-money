# go-payup-airtel-money

Airtel Money Ledger implementation for the go-payup library

#### DSN Endpoints

Staging -- https://openapiuat.airtel.africa/

Production -- https://openapi.airtel.africa/

#### DSN Parameters

| Paramter   | Description                     | Required | Default |
| ---------- | ------------------------------- | -------- | ------- |
| country    | 2 letter ISO country code       | yes      |         |
| currency   | 3 letter ISO currency code      | yes      |         |
| timeout    | http request timeout in seconds | no       | 30      |
| client_id  | Airtel app client ID            | yes      |         |
| secret_key | Airtel app secret key           | yes      |         |


#### Example

###### .ini

```ini
[airtel_money]
dsn = "https://openapiuat.airtel.africa?country=MW&currency=MWK&client_id=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx&secret_key=xxxxxxxx-xxxx-xxxx-xxxx-xxxxxxxxxxxx&timeout=15"
```


```go

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

	dsn = cfg.Section("airtel_money").Key("dsn").String()

	wallet, err = gopayup.NewMobileWallet("airtelMoney", dsn)
	failIfError(t, err)

	transactionID = "TRANS-ID"
	ctx, cancelFn := context.WithTimeout(
		context.Background(),
		30*time.Second,
	)
	defer cancelFn()

	transaction, err = wallet.VerifyTransaction(ctx, transactionID)
	failIfError(t, err)

	t.Log(transaction)
}

```