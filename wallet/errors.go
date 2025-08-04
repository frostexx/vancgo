package wallet

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/txnbuild"
	"github.com/stellar/go/xdr"
)

var ErrUnAuthorized = errors.New("unauthorized")

func getTxErrorFromResultXdr(resultXdr string) error {
	var txResult xdr.TransactionResult
	if err := xdr.SafeUnmarshalBase64(resultXdr, &txResult); err != nil {
		return fmt.Errorf("failed to decode result XDR: %w", err)
	}

	// Transaction-level error
	if txResult.Result.Code != xdr.TransactionResultCodeTxSuccess {
		return fmt.Errorf("transaction failed with code: %s", txResult.Result.Code.String())
	}

	if txResult.Result.Results == nil {
		return fmt.Errorf("transaction succeeded but no operation results returned")
	}

	for i, opResult := range *txResult.Result.Results {
		switch opResult.Tr.Type {
		case xdr.OperationTypePayment:
			if opResult.Tr.PaymentResult == nil {
				return fmt.Errorf("operation %d: missing payment result", i)
			}
			code := opResult.Tr.PaymentResult.Code
			if code != xdr.PaymentResultCodePaymentSuccess {
				return fmt.Errorf("operation %d failed: %s", i, code.String())
			}

		case xdr.OperationTypeClaimClaimableBalance:
			if opResult.Tr.ClaimClaimableBalanceResult == nil {
				return fmt.Errorf("operation %d: missing claim claimable balance result", i)
			}
			code := opResult.Tr.ClaimClaimableBalanceResult.Code
			if code != xdr.ClaimClaimableBalanceResultCodeClaimClaimableBalanceSuccess {
				return fmt.Errorf("operation %d failed: %s", i, code.String())
			}

		default:
			return fmt.Errorf("operation %d has unsupported type: %s", i, opResult.Tr.Type.String())
		}
	}

	return nil
}

func (w *Wallet) Transfer(kp *keypair.Full, amountStr string, address string) error {
	// Parse requested amount first
	requestedAmount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return fmt.Errorf("invalid amount: %w", err)
	}

	// Check if amount is too small or negative
	if requestedAmount <= 0.001 {
		return fmt.Errorf("amount too small to transfer: %.7f PI", requestedAmount)
	}

	w.GetBaseReserve()
	baseReserve := w.baseReserve

	// Get account details
	account, err := w.GetAccount(kp)
	if err != nil {
		return fmt.Errorf("error getting account: %w", err)
	}

	// Get actual native (PI) balance
	var nativeBalance float64
	for _, bal := range account.Balances {
		if bal.Asset.Type == "native" {
			nativeBalance, err = strconv.ParseFloat(bal.Balance, 64)
			if err != nil {
				return fmt.Errorf("invalid balance format: %w", err)
			}
			break
		}
	}

	// Calculate minimum required balance
	minBalance := baseReserve * float64(2+account.SubentryCount)

	// Available balance = total - reserve - transaction fee
	available := nativeBalance - minBalance - 0.01
	if available <= 0 {
		return fmt.Errorf("insufficient available balance")
	}

	// Use the smaller of requested amount or available balance
	transferAmount := requestedAmount
	if transferAmount > available {
		transferAmount = available - 0.001 // Leave small buffer
	}

	// Final check
	if transferAmount <= 0 {
		return fmt.Errorf("insufficient balance for transfer")
	}

	// Build payment operation
	paymentOp := txnbuild.Payment{
		Destination: address,
		Amount:      strconv.FormatFloat(transferAmount, 'f', 7, 64),
		Asset:       txnbuild.NativeAsset{},
	}

	// Build transaction
	txParams := txnbuild.TransactionParams{
		SourceAccount:        &account,
		IncrementSequenceNum: true,
		Operations:           []txnbuild.Operation{&paymentOp},
		BaseFee:              txnbuild.MinBaseFee,
		Preconditions:        txnbuild.Preconditions{TimeBounds: txnbuild.NewInfiniteTimeout()},
	}

	tx, err := txnbuild.NewTransaction(txParams)
	if err != nil {
		return fmt.Errorf("error building transaction: %w", err)
	}

	// Sign and submit
	signedTx, err := tx.Sign(w.networkPassphrase, kp)
	if err != nil {
		return fmt.Errorf("error signing transaction: %w", err)
	}

	resp, err := w.client.SubmitTransaction(signedTx)
	if err != nil {
		return fmt.Errorf("error submitting transaction: %w", err)
	}

	if !resp.Successful {
		return getTxErrorFromResultXdr(resp.ResultXdr)
	}

	fmt.Printf("Transfer successful: %.7f PI - Hash: %s\n", transferAmount, resp.Hash)
	return nil
}

func (w *Wallet) WithdrawClaimableBalance(kp *keypair.Full, amountStr, balanceID, address string) (string, float64, error) {
	amount, err := strconv.ParseFloat(amountStr, 64)
	if err != nil {
		return "", 0, fmt.Errorf("error formatting amount: %s", err.Error())
	}
	amount = amount - 0.01

	hash, err := w.ClaimAndWithdraw(kp, amount, balanceID, address)
	if err != nil {
		return "", amount, fmt.Errorf("error claiming and withdrawing: %v", err)
	}

	return hash, amount, nil
}

func (w *Wallet) ClaimAndWithdraw(kp *keypair.Full, amount float64, balanceID, address string) (string, error) {
	account, err := w.GetAccount(kp)
	if err != nil {
		return "", err
	}

	claimOp := txnbuild.ClaimClaimableBalance{
		BalanceID: balanceID,
	}

	paymentOp := txnbuild.Payment{
		Destination: address,
		Amount:      strconv.FormatFloat(amount, 'f', -1, 64),
		Asset:       txnbuild.NativeAsset{},
	}

	txParams := txnbuild.TransactionParams{
		SourceAccount:        &account,
		IncrementSequenceNum: true,
		Operations:           []txnbuild.Operation{&claimOp, &paymentOp},
		BaseFee:              1_000_000,
		Preconditions: txnbuild.Preconditions{
			TimeBounds: txnbuild.NewInfiniteTimeout(),
		},
	}

	tx, err := txnbuild.NewTransaction(txParams)
	if err != nil {
		return "", fmt.Errorf("error building transaction: %v", err)
	}

	signedTx, err := tx.Sign(w.networkPassphrase, kp)
	if err != nil {
		return "", fmt.Errorf("error signing transaction: %v", err)
	}

	resp, err := w.client.SubmitTransaction(signedTx)
	if err != nil {
		return "", fmt.Errorf("error submitting transaction: %v", err)
	}

	if !resp.Successful {
		return "", fmt.Errorf("transaction failed")
	}

	return resp.Hash, nil
}

func (w *Wallet) CreateClaimable(kp *keypair.Full, recipientAddress string, amount float64) (string, error) {
	senderAccount, err := w.GetAccount(kp)
	if err != nil {
		return "", err
	}

	t := time.Now().Add(10 * time.Minute)
	claimant := txnbuild.Claimant{
		Destination: recipientAddress,
		Predicate:   txnbuild.NotPredicate(txnbuild.BeforeAbsoluteTimePredicate(t.Unix())),
	}

	createOp := txnbuild.CreateClaimableBalance{
		Asset:        txnbuild.NativeAsset{},
		Amount:       fmt.Sprintf("%.2f", amount),
		Destinations: []txnbuild.Claimant{claimant},
	}

	txParams := txnbuild.TransactionParams{
		SourceAccount:        &senderAccount,
		IncrementSequenceNum: true,
		Operations:           []txnbuild.Operation{&createOp},
		BaseFee:              1_000_000, //txnbuild.MinBaseFee,
		Preconditions: txnbuild.Preconditions{
			TimeBounds: txnbuild.NewInfiniteTimeout(),
		},
	}

	tx, err := txnbuild.NewTransaction(txParams)
	if err != nil {
		return "", fmt.Errorf("error building transaction: %v", err)
	}

	signedTx, err := tx.Sign(w.networkPassphrase, kp)
	if err != nil {
		return "", fmt.Errorf("error signing transaction: %v", err)
	}

	resp, err := w.client.SubmitTransaction(signedTx)
	if err != nil {
		return "", fmt.Errorf("error submitting transaction: %v", err)
	}

	return resp.Hash, nil
}