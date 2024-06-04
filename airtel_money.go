package gopayupairtelmoney

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"
	gopayup "github.com/sharkfoursix/go-payup"
	"github.com/sharkfoursix/go-payup/pkg"
)

func init() {
	gopayup.RegisterWallet("airtelMoney", newAirtelWallet)
}

var (
	apiErrors map[string][]string = map[string][]string{
		"DP00800001000": {"Ambiguous", "The transaction is still processing and is in ambiguous state. Please do the transaction enquiry to fetch the transaction status."},
		"DP00800001001": {"Success", "Transaction is successful."},
		"DP00800001002": {"Incorrect Pin", "Incorrect pin has been entered."},
		"DP00800001003": {"Exceeds withdrawal amount limit(s) / Withdrawal amount limit exceeded", "The User has exceeded their wallet allowed transaction limit."},
		"DP00800001004": {"Invalid Amount", "The amount User is trying to transfer is less than the minimum amount allowed."},
		"DP00800001005": {"Transaction ID is invalid", "User didn't enter the pin."},
		"DP00800001006": {"In process", "Transaction in pending state. Please check after sometime."},
		"DP00800001007": {"Not enough balance", "User wallet does not have enough money to cover the payable amount."},
		"DP00800001008": {"Refused", "The transaction was refused."},
		"DP00800001010": {"Transaction not permitted to Payee", "Payee is already initiated for churn or barred or not registered on Airtel Money platform."},
		"DP00800001024": {"Transaction Timed Out", "The transaction was timed out."},
		"DP00800001025": {"Transaction Not Found", "The transaction was not found."},
		"DP00800001026": {"Forbidden", "X-signature and payload did not match."},
		"DP00800001029": {"Transaction Expired", "Transaction has been expired."},
	}

	errorMap map[string]pkg.TransactionStatus = map[string]pkg.TransactionStatus{
		"TS":  pkg.TS_SUCCESS,
		"TF":  pkg.TS_FAILED,
		"TA":  pkg.TS_PENDING, // ambiguous state
		"TIP": pkg.TS_PENDING,
		"TE":  pkg.TS_EXPIRED,
	}
)

func newAirtelWallet(dsn string) (pkg.MobileWallet, error) {
	wallet := AirtelMoneyWallet{}

	u, err := url.Parse(dsn)
	if err != nil {
		return nil, errors.Wrap(err, "parsing dsn")
	}

	// extract the following:
	//	* client_id
	//	* secret_key
	//	* timeout (seconds)
	//  * country ISO code
	//	* currency ISO code
	values, err := url.ParseQuery(u.RawQuery)
	if err != nil {
		return nil, errors.Wrap(err, "reading client parameters")
	}

	requiredValues := []string{"client_id", "secret_key", "country", "currency"}
	for _, v := range requiredValues {
		if !values.Has(v) {
			return nil, errors.Errorf("missing parameter `%s`", v)
		}
	}

	wallet.clientID = values.Get("client_id")
	wallet.clientSecret = values.Get("secret_key")
	wallet.country = values.Get("country")
	wallet.currency = values.Get("currency")

	if values.Has("timeout") {
		timeoutValue := values.Get("timeout")
		timeout, err := strconv.ParseInt(timeoutValue, 10, 32)
		if err != nil {
			return nil, errors.Wrapf(err, "invalid timeout value %s", timeoutValue)
		}
		wallet.timeout = int(timeout)
	}else{
		wallet.timeout = 30 * 1000 // seconds
	}

	u.RawQuery = ""
	wallet.endpoint = u.String()
	return &wallet, nil
}

type RequestData map[string]any

type authToken struct {
	AccessToken string     `json:"access_token"`
	ExpiresIn   string     `json:"expires_in"` // token expiration
	TokenType   string     `json:"token_type"`
	ExpiresAt   *time.Time `json:"-"`
}

func (at authToken) Valid() bool {
	if at.ExpiresAt == nil {
		return false
	}
	now := time.Now().UTC()
	return now.Before(*at.ExpiresAt)
}

type AirtelMoneyWallet struct {
	timeout      int
	clientID     string
	endpoint     string
	accessToken  authToken
	clientSecret string
	country      string
	currency     string
}

func (amw AirtelMoneyWallet) path(p string, other ...string) string {
	return amw.endpoint + "/" + p + "/" + strings.Join(other, "/")
}

func (amw *AirtelMoneyWallet) authenticate() error {
	var (
		err          error
		httpRequest  *http.Request
		httpResponse *http.Response
		ctx          context.Context
		cancelFunc   context.CancelFunc
	)

	if amw.isAuthenticated() {
		return nil
	}

	ctx, cancelFunc = context.WithTimeout(context.Background(), time.Duration(amw.timeout)*time.Millisecond)
	defer cancelFunc()

	postData, _ := json.Marshal(&RequestData{
		"client_id":  amw.clientID,
		"secret_key": amw.clientSecret,
		"grant_type": "",
	})

	httpRequest, err = http.NewRequestWithContext(ctx, "POST", amw.path("auth/oauth2/token"), bytes.NewReader(postData))
	if err != nil {
		return errors.Wrap(err, "creating authentication request")
	}

	httpRequest.Header.Add("Accept", "application/json")
	httpRequest.Header.Add("Content-Type", "application/json")

	httpResponse, err = http.DefaultClient.Do(httpRequest)
	if err != nil {
		return errors.Wrap(err, "sending authentication request")
	}

	if httpResponse.StatusCode != 200 {
		return errors.Errorf("authentication response code %d", httpResponse.StatusCode)
	}

	responseBytes, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return errors.Wrap(err, "reading authentication response")
	}
	err = json.Unmarshal(responseBytes, &amw.accessToken)
	if err != nil {
		return errors.Wrap(err, "parsing authentication response")
	}
	timeout, err := strconv.Atoi(amw.accessToken.ExpiresIn)
	if err != nil {
		return errors.Wrap(err, "parsing authentication response(timeout value)")
	}
	then := time.Now().UTC().Add(time.Duration(timeout) * time.Second)
	amw.accessToken.ExpiresAt = &then
	return nil
}

func (amw AirtelMoneyWallet) isAuthenticated() bool {
	return amw.accessToken.Valid()
}

func (wallet *AirtelMoneyWallet) VerifyTransaction(ctx context.Context, txnid string) (pkg.Transaction, error) {
	var (
		err          error
		httpRequest  *http.Request
		httpResponse *http.Response
		timedCtx     context.Context
		cancelFunc   context.CancelFunc
		transaction  TransactionDetails
	)
	err = wallet.authenticate()
	if err != nil {
		return nil, err
	}

	timedCtx, cancelFunc = context.WithTimeout(ctx, time.Duration(wallet.timeout)*time.Millisecond)
	defer cancelFunc()

	httpRequest, err = http.NewRequestWithContext(timedCtx, "GET", wallet.path("standard/v1/payments", txnid), nil)
	if err != nil {
		return nil, errors.Wrap(err, "creating transaction verification request")
	}

	httpRequest.Header.Add("Accept", "application/json")
	httpRequest.Header.Add("X-Country", wallet.country)
	httpRequest.Header.Add("X-Currency", wallet.currency)
	httpRequest.Header.Add("Content-Type", "application/json")
	httpRequest.Header.Add("Authorization", fmt.Sprintf("Bearer %s", wallet.accessToken.AccessToken))

	httpResponse, err = http.DefaultClient.Do(httpRequest)
	if err != nil {
		return nil, errors.Wrap(err, "sending transaction verification request")
	}

	if httpResponse.StatusCode == 404 {
		return nil, pkg.ErrTransactionNotFound
	} else {
		if httpResponse.StatusCode != 200 {
			return nil, errors.Errorf("transaction response code %d", httpResponse.StatusCode)
		}
	}
	responseBytes, err := io.ReadAll(httpResponse.Body)
	if err != nil {
		return nil, errors.Wrap(err, "reading transaction verification response")
	}
	err = json.Unmarshal(responseBytes, &transaction)
	if err != nil {
		return nil, errors.Wrap(err, "parsing transaction verification response")
	}

	if transaction.RequestStatus.Code == "400" {
		return nil, pkg.ErrTransactionNotFound
	} else {
		if transaction.RequestStatus.Code != "200" {
			return nil, errors.Errorf("transaction response code %s", transaction.RequestStatus.Code)
		}
	}

	return &transaction, nil
}

type TransactionDetails struct {
	Data          Data   `json:"data"`
	RequestStatus Status `json:"status"`
}
type Transaction struct {
	AirtelMoneyID string `json:"airtel_money_id"`
	PartnerID     string `json:"id"` // Transaction Id generated by TPP / Aggregator / Merchant.
	Message       string `json:"message"`
	TxnStatus     string `json:"status"` // Possible Status: TF (Transaction Failed) TS (Transaction Success) TA (Transaction Ambiguous) TIP (Transaction in Progress) TE (Transaction Expired)
}
type Data struct {
	Transaction Transaction `json:"transaction"`
}
type Status struct {
	Code         string `json:"code"`
	Message      string `json:"message"`
	ResultCode   string `json:"result_code"`
	ResponseCode string `json:"response_code"`
	Success      bool   `json:"success"`
}

func (s Status) ResponseCodeReason() string {
	return apiErrors[s.ResponseCode][0]
}

func (s Status) ResponseCodeDescription() string {
	return apiErrors[s.ResponseCode][1]
}

func (d TransactionDetails) ID() string {
	return d.Data.Transaction.AirtelMoneyID
}
func (d TransactionDetails) RefID() string {
	return d.Data.Transaction.PartnerID
}

func (d TransactionDetails) Amount() float64 {
	return 0
}

func (d TransactionDetails) Status() pkg.TransactionStatus {
	return errorMap[d.Data.Transaction.TxnStatus]
}

func (t TransactionDetails) CreatedAt() *time.Time {
	return nil
}
