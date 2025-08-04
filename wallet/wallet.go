package wallet

import (
	"fmt"
	"os"
	"pi/util"
	"strconv"

	"github.com/stellar/go/clients/horizonclient"
	hClient "github.com/stellar/go/clients/horizonclient"
	"github.com/stellar/go/keypair"
	"github.com/stellar/go/protocols/horizon"
	"github.com/stellar/go/protocols/horizon/operations"
	"github.com/stellar/go/txnbuild"
)

type Wallet struct {
	networkPassphrase string
	serverURL         string
	client            *hClient.Client
	baseReserve       float64
}

func New() *Wallet {
	client := hClient.DefaultPublicNetClient
	client.HorizonURL = os.Getenv("NET_URL")

	w := &Wallet{
		networkPassphrase: os.Getenv("NET_PASSPHRASE"),
		serverURL:         os.Getenv("NET_URL"),
		client:            client,
		baseReserve:       0.49,
	}
	w.GetBaseReserve()

	return w
}

func (w *Wallet) GetBaseReserve() {
	ledger, err := w.client.Ledgers(horizonclient.LedgerRequest{Order: horizonclient.OrderDesc, Limit: 1})
	if err != nil {
		fmt.Println(err)
		return
	}

	if len(ledger.Embedded.Records) == 0 {
		fmt.Println("No ledger records found")
		return
	}

	baseReserveStr := ledger.Embedded.Records[0].BaseReserve
	w.baseReserve = float64(baseReserveStr) / 1e7
	fmt.Printf("Base reserve: %.7f\n", w.baseReserve)
}

func (w *Wallet) GetAddress(kp *keypair.Full) string {
	return kp.Address()
}

func (w *Wallet) Login(seedPhrase string) (*keypair.Full, error) {
	kp, err := util.GetKeyFromSeed(seedPhrase)
	if err != nil {
		return nil, err
	}

	return kp, nil
}

func (w *Wallet) GetAccount(kp *keypair.Full) (horizon.Account, error) {
	accReq := hClient.AccountRequest{AccountID: kp.Address()}
	account, err := w.client.AccountDetail(accReq)
	if err != nil {
		return horizon.Account{}, fmt.Errorf("error fetching account details: %v", err)
	}

	return account, nil
}

func (w *Wallet) GetAvailableBalance(kp *keypair.Full) (string, error) {
	account, err := w.GetAccount(kp)
	if err != nil {
		return "", err
	}

	for _, balance := range account.Balances {
		if balance.Asset.Type == "native" {
			return balance.Balance, nil
		}
	}

	return "0", nil
}

func (w *Wallet) GetTransactions(kp *keypair.Full, limit uint) ([]operations.Operation, error) {
	req := horizonclient.OperationRequest{
		ForAccount: kp.Address(),
		Limit:      limit,
		Order:      horizonclient.OrderDesc,
	}

	ops, err := w.client.Operations(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching transactions: %w", err)
	}

	return ops.Embedded.Records, nil
}

func (w *Wallet) GetLockedBalances(kp *keypair.Full) ([]horizon.ClaimableBalance, error) {
	req := horizonclient.ClaimableBalanceRequest{
		Claimant: kp.Address(),
	}

	balances, err := w.client.ClaimableBalances(req)
	if err != nil {
		return nil, fmt.Errorf("error fetching locked balances: %w", err)
	}

	return balances.Embedded.Records, nil
}

func (w *Wallet) GetClaimableBalance(balanceID string) (*horizon.ClaimableBalance, error) {
	balance, err := w.client.ClaimableBalance(balanceID)
	if err != nil {
		return nil, fmt.Errorf("error fetching claimable balance: %w", err)
	}

	return &balance, nil
}

func (w *Wallet) Transfer(kp *keypair.Full, amountStr, destinationAddr string) error {
	account, err := w.GetAccount(kp)
	if err != nil {
		return fmt.Errorf("error getting account: %w", err)
	}

	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return fmt.Errorf("invalid amount: %w", err)
	}

	paymentOp := &txnbuild.Payment{
		Destination: destinationAddr,
		Amount:      fmt.Sprintf("%.7f", amount),
		Asset:       txnbuild.NativeAsset{},
	}

	tx, err := txnbuild.NewTransaction(
		txnbuild.TransactionParams{
			SourceAccount:        &account,
			IncrementSequenceNum: true,
			Operations:           []txnbuild.Operation{paymentOp},
			BaseFee:              txnbuild.MinBaseFee,
			Preconditions: txnbuild.Preconditions{
				TimeBounds: txnbuild.NewInfiniteTimeout(),
			},
		},
	)
	if err != nil {
		return fmt.Errorf("error building transaction: %w", err)
	}

	tx, err = tx.Sign(w.networkPassphrase, kp)
	if err != nil {
		return fmt.Errorf("error signing transaction: %w", err)
	}

	_, err = w.client.SubmitTransaction(tx)
	if err != nil {
		return fmt.Errorf("transaction failed: %w", err)
	}

	return nil
}

func (w *Wallet) WithdrawClaimableBalance(kp *keypair.Full, amountStr, balanceID, destinationAddr string) (string, float64, error) {
	account, err := w.GetAccount(kp)
	if err != nil {
		return "", 0, fmt.Errorf("error getting account: %w", err)
	}

	// Create claim operation
	claimOp := &txnbuild.ClaimClaimableBalance{
		BalanceID: balanceID,
	}

	// Use competitive fee
	fee := util.GetCompetitiveFee()

	// Build transaction
	tx, err := txnbuild.NewTransaction(
		txnbuild.TransactionParams{
			SourceAccount:        &account,
			IncrementSequenceNum: true,
			Operations:           []txnbuild.Operation{claimOp},
			BaseFee:              fee,
			Preconditions: txnbuild.Preconditions{
				TimeBounds: txnbuild.NewInfiniteTimeout(),
			},
		},
	)
	if err != nil {
		return "", 0, fmt.Errorf("error building transaction: %w", err)
	}

	// Sign transaction
	tx, err = tx.Sign(w.networkPassphrase, kp)
	if err != nil {
		return "", 0, fmt.Errorf("error signing transaction: %w", err)
	}

	// Submit transaction
	resp, err := w.client.SubmitTransaction(tx)
	if err != nil {
		return "", 0, fmt.Errorf("transaction failed: %w", err)
	}

	// Parse amount from response if available
	amount, _ := strconv.ParseFloat(amountStr, 64)

	return resp.Hash, amount, nil
}